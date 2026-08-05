package main

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httputil"
	"os"
	"runtime/debug"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"github.com/tinfoilsh/tinfoil-go"
	verifierclient "github.com/tinfoilsh/tinfoil-go/verifier/client"
)

const (
	httpReadHeaderTimeout  = 30 * time.Second
	httpIdleTimeout        = 120 * time.Second
	httpMaxHeaderBytes     = 1 << 20
	handshakeTimeout       = 60 * time.Second
	maxTokenUsageBodySize  = 8 << 20
	upstreamReloadCooldown = 10 * time.Second
	proxyInstanceIDBytes   = 16
	verificationPath       = "/verification-document"
	proxySoftwareName      = "tinfoil-proxy"
)

type readyMessage struct {
	Event                string                     `json:"event"`
	InstanceID           string                     `json:"instance_id"`
	Enclave              string                     `json:"enclave"`
	Repo                 string                     `json:"repo"`
	Listen               string                     `json:"listen"`
	VerificationDocument *proxyVerificationDocument `json:"verification_document"`
}

type proxyRuntime struct {
	InstanceID string                          `json:"instanceId"`
	Listener   string                          `json:"listener"`
	Software   verifierclient.SoftwareIdentity `json:"software"`
}

type proxyVerificationDocument struct {
	*verifierclient.VerificationDocument
	Runtime proxyRuntime `json:"runtime"`
}

type verificationMessage struct {
	Event                string                     `json:"event"`
	VerificationDocument *proxyVerificationDocument `json:"verification_document"`
}

type tokenStatsMessage struct {
	Event        string `json:"event"`
	Upstreamed   uint64 `json:"upstreamed"`
	Downstreamed uint64 `json:"downstreamed"`
}

type tokenStatsEmitter func(tokenStatsMessage) error

var stdoutMu sync.Mutex
var buildVersion = "unknown"

func emitJSONLine(msg any) error {
	payload, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	stdoutMu.Lock()
	defer stdoutMu.Unlock()
	if _, err := fmt.Fprintln(os.Stdout, string(payload)); err != nil {
		return err
	}
	return nil
}

func emitReady(instanceID, enclave, repo, listen string, document *proxyVerificationDocument) error {
	msg := readyMessage{
		Event:                "ready",
		InstanceID:           instanceID,
		Enclave:              enclave,
		Repo:                 repo,
		Listen:               listen,
		VerificationDocument: document,
	}
	return emitJSONLine(msg)
}

func emitVerification(document *proxyVerificationDocument) error {
	return emitJSONLine(verificationMessage{
		Event:                "verification",
		VerificationDocument: document,
	})
}

func emitTokenStats(msg tokenStatsMessage) error {
	return emitJSONLine(msg)
}

func waitForGoSignal(timeout time.Duration) error {
	result := make(chan error, 1)
	go func() {
		reader := bufio.NewReader(os.Stdin)
		line, err := reader.ReadString('\n')
		if err != nil {
			result <- fmt.Errorf("handshake stdin closed: %w", err)
			return
		}
		switch strings.TrimSpace(line) {
		case "go":
			result <- nil
		case "abort":
			result <- errors.New("aborted by parent")
		default:
			result <- fmt.Errorf("unexpected handshake signal %q", strings.TrimSpace(line))
		}
	}()
	select {
	case err := <-result:
		return err
	case <-time.After(timeout):
		return fmt.Errorf("handshake signal not received within %s", timeout)
	}
}

func runProxy(cmd *cobra.Command, args []string) error {
	setupLogger()
	warnIfNonLoopbackBind()

	log.WithFields(log.Fields{
		"enclave_host": enclaveHost,
		"repo":         repo,
	}).Info("initializing secure client")

	requestedEnclave, requestedRepo := enclaveHost, repo
	initial, err := buildUpstream(requestedEnclave, requestedRepo)
	if err != nil {
		log.WithError(err).Error("failed to create HTTP client")
		return err
	}
	enclaveHost = initial.host
	repo = initial.repo
	log.Debug("secure HTTP client created successfully")

	reloading := newReloadingUpstream(initial, func() (*upstream, error) {
		return buildUpstream(requestedEnclave, requestedRepo)
	})

	var tokens *tokenCounter
	if handshake {
		tokens = newTokenCounter(emitTokenStats)
	}

	cacheSecret := resolveUserCacheSecret(userCacheSecret, cmd.Flags().Changed(userCacheSecretFlag))
	proxy := newReverseProxy(reloading, cacheSecret, tokens)

	addr := bindAddress()
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		log.WithError(err).Error("failed to bind listener")
		return err
	}
	listenAddr := listener.Addr().String()
	instanceID, err := newProxyInstanceID()
	if err != nil {
		listener.Close()
		return err
	}
	runtime := proxyRuntime{
		InstanceID: instanceID,
		Listener:   listenAddr,
		Software: verifierclient.SoftwareIdentity{
			Name:    proxySoftwareName,
			Version: proxyVersion(),
		},
	}
	if handshake {
		reloading.onVerificationChange = func(document *verifierclient.VerificationDocument) {
			if err := emitVerification(&proxyVerificationDocument{
				VerificationDocument: document,
				Runtime:              runtime,
			}); err != nil {
				log.WithError(err).Warn("failed to emit updated verification document")
			}
		}
	}
	mux := http.NewServeMux()
	mux.Handle(verificationPath, verificationDocumentHandler(reloading, runtime))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		proxy.ServeHTTP(w, r)
	})

	if handshake {
		document, ok := currentVerificationDocument(reloading, runtime)
		if !ok {
			listener.Close()
			return errors.New("verification document unavailable after successful verification")
		}
		if err := emitReady(instanceID, enclaveHost, repo, listenAddr, document); err != nil {
			listener.Close()
			return err
		}
		if err := waitForGoSignal(handshakeTimeout); err != nil {
			listener.Close()
			return err
		}
	}

	log.WithFields(log.Fields{
		"address":      listenAddr,
		"enclave_host": enclaveHost,
	}).Info("starting HTTP proxy server")
	server := &http.Server{
		Addr:              addr,
		Handler:           localOnlyGuard(listenerGuardAddress(addr, listenAddr), mux),
		ReadHeaderTimeout: httpReadHeaderTimeout,
		IdleTimeout:       httpIdleTimeout,
		MaxHeaderBytes:    httpMaxHeaderBytes,
	}
	return server.Serve(listener)
}

func listenerGuardAddress(configuredAddr, listenAddr string) string {
	host, _, configuredErr := net.SplitHostPort(configuredAddr)
	_, port, listenErr := net.SplitHostPort(listenAddr)
	if configuredErr != nil || listenErr != nil {
		return listenAddr
	}
	return net.JoinHostPort(host, port)
}

// newReverseProxy assembles the forwarding pipeline. The logging transport
// wraps the cache-secret injector, which wraps the reloading upstream, so the
// injected field survives router-reselection retries and is sealed by the
// pinned connection beneath before it leaves the machine.
func newReverseProxy(reloading *reloadingUpstream, cacheSecret string, tokens *tokenCounter) *httputil.ReverseProxy {
	var transport http.RoundTripper = reloading
	if cacheSecret != "" {
		transport = &userCacheSecretTransport{secret: cacheSecret, transport: transport}
	}
	return &httputil.ReverseProxy{
		Director: func(req *http.Request) {
			req.URL.Scheme = "https"
			host := reloading.get().host
			req.URL.Host = host
			req.Host = host
			if _, ok := req.Header["User-Agent"]; !ok {
				// Match httputil.NewSingleHostReverseProxy: suppress the
				// default Go client User-Agent instead of advertising it.
				req.Header.Set("User-Agent", "")
			}
		},
		Transport: withLoggingTransport(log.StandardLogger(), transport, tokens),
	}
}

type upstream struct {
	host                 string
	repo                 string
	transport            http.RoundTripper
	verificationDocument func() *verifierclient.VerificationDocument
}

// buildUpstream verifies and pins a router. When no enclave host is pinned via
// flags, the SDK reselects a healthy router from the router service, which is
// what lets the proxy recover when the current router rotates or goes down.
func buildUpstream(requestedEnclave, requestedRepo string) (*upstream, error) {
	// TLS pinning keeps response bodies unencrypted at the HTTP layer, so the
	// reverse proxy forwards accurate framing headers.
	opts := []tinfoil.ClientOption{tinfoil.WithTransport(tinfoil.TransportTLS)}
	if requestedEnclave != "" || requestedRepo != "" {
		opts = append(opts, tinfoil.WithEnclave(requestedEnclave), tinfoil.WithRepo(requestedRepo))
	}
	tinfoilClient, err := tinfoil.NewClientWithOptions(opts...)
	if err != nil {
		return nil, err
	}
	return &upstream{
		host:                 tinfoilClient.Enclave(),
		repo:                 tinfoilClient.Repo(),
		transport:            tinfoilClient.HTTPClient().Transport,
		verificationDocument: tinfoilClient.VerificationDocument,
	}, nil
}

func newProxyInstanceID() (string, error) {
	id := make([]byte, proxyInstanceIDBytes)
	if _, err := rand.Read(id); err != nil {
		return "", fmt.Errorf("generate proxy instance ID: %w", err)
	}
	return hex.EncodeToString(id), nil
}

func proxyVersion() string {
	if buildVersion != "unknown" {
		return strings.TrimPrefix(buildVersion, "v")
	}
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
		return strings.TrimPrefix(info.Main.Version, "v")
	}
	return "devel"
}

func verificationDocumentHandler(reloading *reloadingUpstream, runtime proxyRuntime) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		document, ok := currentVerificationDocument(reloading, runtime)
		if !ok {
			http.Error(w, "verification document unavailable", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(document); err != nil {
			log.WithError(err).Warn("failed to write verification document")
		}
	})
}

func currentVerificationDocument(reloading *reloadingUpstream, runtime proxyRuntime) (*proxyVerificationDocument, bool) {
	current := reloading.get()
	if current.verificationDocument == nil {
		return nil, false
	}
	document := current.verificationDocument()
	if document == nil {
		return nil, false
	}
	return &proxyVerificationDocument{
		VerificationDocument: document,
		Runtime:              runtime,
	}, true
}

var errReloadCoolingDown = errors.New("upstream reload attempted too recently")

// reloadingUpstream routes requests to the current upstream and, when a
// request fails at the transport level, rebuilds the secure client (rerunning
// router selection and attestation) and retries the request when it is
// idempotent and its body can be replayed. This keeps the proxy working
// across router rotations and outages without a restart.
type reloadingUpstream struct {
	build func() (*upstream, error)

	mu      sync.RWMutex
	current *upstream

	reloadMu    sync.Mutex
	lastAttempt time.Time

	documentMu           sync.Mutex
	lastVerifiedAt       string
	onVerificationChange func(*verifierclient.VerificationDocument)
}

func newReloadingUpstream(initial *upstream, build func() (*upstream, error)) *reloadingUpstream {
	reloading := &reloadingUpstream{build: build, current: initial}
	if initial.verificationDocument != nil {
		if document := initial.verificationDocument(); document != nil {
			reloading.lastVerifiedAt = document.VerifiedAt
		}
	}
	return reloading
}

func (r *reloadingUpstream) get() *upstream {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.current
}

func (r *reloadingUpstream) RoundTrip(req *http.Request) (*http.Response, error) {
	current := r.get()
	resp, err := current.transport.RoundTrip(requestForHost(req, current.host))
	r.notifyVerificationChange(current)
	if err == nil {
		return resp, nil
	}
	if req.Context().Err() != nil {
		return resp, err
	}

	next, reloadErr := r.reload(current)
	if reloadErr != nil {
		return nil, err
	}
	retry, ok := replayableRequest(req, next.host)
	if !ok {
		return nil, err
	}
	resp, err = next.transport.RoundTrip(retry)
	r.notifyVerificationChange(next)
	return resp, err
}

// reload swaps in a freshly verified upstream. Concurrent failing requests
// serialize here so only one rebuild runs, and a cooldown prevents hammering
// the router service and attestation endpoints when upstream stays down.
func (r *reloadingUpstream) reload(failed *upstream) (*upstream, error) {
	r.reloadMu.Lock()
	defer r.reloadMu.Unlock()

	current := r.get()
	if current != failed {
		return current, nil
	}
	if time.Since(r.lastAttempt) < upstreamReloadCooldown {
		return nil, errReloadCoolingDown
	}
	r.lastAttempt = time.Now()

	log.WithField("enclave_host", failed.host).Warn("upstream request failed, reselecting router")
	next, err := r.build()
	if err != nil {
		log.WithError(err).Error("router reselection failed")
		return nil, err
	}
	r.mu.Lock()
	r.current = next
	r.mu.Unlock()
	r.notifyVerificationChange(next)
	log.WithField("enclave_host", next.host).Info("router reselection succeeded")
	return next, nil
}

func (r *reloadingUpstream) notifyVerificationChange(current *upstream) {
	r.documentMu.Lock()
	defer r.documentMu.Unlock()
	if r.get() != current {
		return
	}
	if current.verificationDocument == nil {
		return
	}
	document := current.verificationDocument()
	if document == nil || document.VerifiedAt == "" {
		return
	}
	if document.VerifiedAt == r.lastVerifiedAt {
		return
	}
	r.lastVerifiedAt = document.VerifiedAt
	if r.onVerificationChange != nil {
		r.onVerificationChange(document)
	}
}

func requestForHost(req *http.Request, host string) *http.Request {
	out := req.Clone(req.Context())
	out.URL.Host = host
	out.Host = host
	return out
}

// idempotentRequest mirrors net/http's request replayability rules. A
// transport error can surface after the router already processed the request,
// so only requests that are safe to execute twice may be retried
// automatically.
func idempotentRequest(req *http.Request) bool {
	switch req.Method {
	case http.MethodGet, http.MethodHead, http.MethodOptions, http.MethodTrace:
		return true
	}
	return req.Header.Get("Idempotency-Key") != "" || req.Header.Get("X-Idempotency-Key") != ""
}

func replayableRequest(req *http.Request, host string) (*http.Request, bool) {
	if !idempotentRequest(req) {
		return nil, false
	}
	retry := requestForHost(req, host)
	if req.Body == nil || req.Body == http.NoBody {
		return retry, true
	}
	if req.GetBody == nil {
		return nil, false
	}
	body, err := req.GetBody()
	if err != nil {
		return nil, false
	}
	retry.Body = body
	return retry, true
}

func allowedHosts(addr string) map[string]struct{} {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return map[string]struct{}{addr: {}}
	}
	allowed := map[string]struct{}{net.JoinHostPort(host, port): {}}
	if loopbackBinds[host] || isUnspecifiedBind(host) {
		for alias := range loopbackBinds {
			allowed[net.JoinHostPort(alias, port)] = struct{}{}
		}
	}
	if isUnspecifiedBind(host) {
		if hostname, err := os.Hostname(); err == nil && hostname != "" {
			addAllowedHost(allowed, hostname, port)
		}
	}
	for _, host := range allowedHostnames {
		addAllowedHost(allowed, host, port)
	}
	return allowed
}

func addAllowedHost(allowed map[string]struct{}, host, port string) {
	if _, _, err := net.SplitHostPort(host); err == nil {
		allowed[host] = struct{}{}
		return
	}
	allowed[net.JoinHostPort(host, port)] = struct{}{}
}

func isUnspecifiedBind(host string) bool {
	ip := net.ParseIP(host)
	return ip != nil && ip.IsUnspecified()
}

func localOnlyGuard(addr string, next http.Handler) http.Handler {
	hosts := allowedHosts(addr)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := hosts[r.Host]; !ok {
			http.Error(w, "invalid Host header", http.StatusBadRequest)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func setupLogger() {
	if trace {
		log.SetLevel(log.TraceLevel)
	} else if verbose {
		log.SetLevel(log.InfoLevel)
	}
	if logFormat == "json" {
		log.SetFormatter(&log.JSONFormatter{})
	} else {
		log.SetFormatter(&log.TextFormatter{})
	}
}

func withLoggingTransport(logger *log.Logger, base http.RoundTripper, tokens *tokenCounter) http.RoundTripper {
	if base == nil {
		base = http.DefaultTransport
	}
	return &loggingTransport{
		wrapped: base,
		logger:  logger,
		tokens:  tokens,
	}
}

// loggingTransport implements http.RoundTripper and wraps an existing
// transport with logging functions
type loggingTransport struct {
	wrapped http.RoundTripper
	logger  *log.Logger
	tokens  *tokenCounter
}

func (lt *loggingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if lt.tokens != nil {
		if err := ensureStreamUsageIncluded(req); err != nil {
			lt.logger.WithError(err).Error("failed to request streamed token usage")
			return nil, err
		}
	}

	lt.logger.WithFields(log.Fields{
		"method": req.Method,
		"host":   req.URL.Host,
		"path":   req.URL.Path,
	}).Debug("Outgoing request to upstream")

	resp, err := lt.wrapped.RoundTrip(req)
	if err != nil {
		lt.logger.WithFields(log.Fields{
			"method": req.Method,
			"host":   req.URL.Host,
			"path":   req.URL.Path,
		}).Error("Request to upstream failed")
		return nil, err
	}

	logEntry := lt.logger.WithFields(log.Fields{
		"method": req.Method,
		"target": req.URL.Host,
		"path":   req.URL.Path,
		"status": resp.Status,
		"size":   resp.ContentLength,
	})

	switch {
	case resp.StatusCode >= 500:
		logEntry.Warn("Upstream server error")
	case resp.StatusCode >= 400:
		logEntry.Warn("Upstream client error")
	default:
		logEntry.Info("Upstream request complete")
	}

	if lt.tokens != nil && resp.Body != nil {
		resp.Body = newUsageTrackingBody(resp.Body, resp.Header.Get("Content-Type"), lt.tokens)
	}

	return resp, err
}

func ensureStreamUsageIncluded(req *http.Request) error {
	if req.Method != http.MethodPost || req.Body == nil || req.Body == http.NoBody {
		return nil
	}
	if !strings.HasSuffix(req.URL.Path, "/chat/completions") {
		return nil
	}
	if req.ContentLength <= 0 || req.ContentLength > maxTokenUsageBodySize {
		return nil
	}

	contentType := strings.ToLower(req.Header.Get("Content-Type"))
	if contentType != "" && !strings.Contains(contentType, "application/json") {
		return nil
	}

	body, readErr := io.ReadAll(req.Body)
	closeErr := req.Body.Close()
	if readErr != nil {
		return readErr
	}
	if closeErr != nil {
		return closeErr
	}
	setRequestBody(req, body)

	if !utf8.Valid(body) {
		// encoding/json would coerce invalid UTF-8 inside strings to U+FFFD
		// on re-marshal, corrupting the client's bytes: forward untouched.
		return nil
	}
	dec := json.NewDecoder(bytes.NewReader(body))
	// Preserve number precision across the re-marshal: int64-range values
	// such as seed would otherwise round-trip through float64 and corrupt.
	dec.UseNumber()
	var payload map[string]any
	if err := dec.Decode(&payload); err != nil || !decodeConsumedAll(dec) {
		return nil
	}
	stream, ok := payload["stream"].(bool)
	if !ok || !stream {
		return nil
	}

	streamOptions, ok := payload["stream_options"].(map[string]any)
	if !ok {
		if _, exists := payload["stream_options"]; exists {
			return nil
		}
		streamOptions = map[string]any{}
		payload["stream_options"] = streamOptions
	}
	if _, exists := streamOptions["include_usage"]; exists {
		return nil
	}
	streamOptions["include_usage"] = true

	updated, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	setRequestBody(req, updated)
	return nil
}

func setRequestBody(req *http.Request, body []byte) {
	req.Body = io.NopCloser(bytes.NewReader(body))
	req.ContentLength = int64(len(body))
	req.GetBody = func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(body)), nil
	}
}

type tokenCounter struct {
	mu           sync.Mutex
	upstreamed   uint64
	downstreamed uint64
	emit         tokenStatsEmitter
	emitCh       chan tokenStatsMessage
}

func newTokenCounter(emit tokenStatsEmitter) *tokenCounter {
	c := &tokenCounter{emit: emit}
	if emit != nil {
		c.emitCh = make(chan tokenStatsMessage, 1)
		go c.emitLoop()
	}
	return c
}

func (c *tokenCounter) add(upstreamed, downstreamed uint64) {
	if c == nil || (upstreamed == 0 && downstreamed == 0) {
		return
	}

	c.mu.Lock()
	c.upstreamed += upstreamed
	c.downstreamed += downstreamed
	msg := tokenStatsMessage{
		Event:        "tokens",
		Upstreamed:   c.upstreamed,
		Downstreamed: c.downstreamed,
	}
	if c.emitCh != nil {
		c.queueEmitLocked(msg)
	}
	c.mu.Unlock()
}

func (c *tokenCounter) queueEmitLocked(msg tokenStatsMessage) {
	select {
	case c.emitCh <- msg:
	default:
		select {
		case <-c.emitCh:
		default:
		}
		c.emitCh <- msg
	}
}

func (c *tokenCounter) emitLoop() {
	for msg := range c.emitCh {
		if err := c.emit(msg); err != nil {
			log.WithError(err).Warn("failed to emit token stats")
		}
	}
}

type responseTokenUsage struct {
	counter          *tokenCounter
	lastUpstreamed   uint64
	lastDownstreamed uint64
}

func (u *responseTokenUsage) applyPayload(payload []byte) {
	if u.counter == nil {
		return
	}
	upstreamed, downstreamed, ok := extractTokenUsage(payload)
	if !ok {
		return
	}

	var upstreamDelta uint64
	if upstreamed > u.lastUpstreamed {
		upstreamDelta = upstreamed - u.lastUpstreamed
	}
	var downstreamDelta uint64
	if downstreamed > u.lastDownstreamed {
		downstreamDelta = downstreamed - u.lastDownstreamed
	}
	u.lastUpstreamed = upstreamed
	u.lastDownstreamed = downstreamed
	u.counter.add(upstreamDelta, downstreamDelta)
}

type usageTrackingBody struct {
	body      io.ReadCloser
	parser    *tokenUsageParser
	finalized bool
}

func newUsageTrackingBody(body io.ReadCloser, contentType string, counter *tokenCounter) io.ReadCloser {
	return &usageTrackingBody{
		body: body,
		parser: &tokenUsageParser{
			stream: strings.Contains(strings.ToLower(contentType), "text/event-stream"),
			usage:  responseTokenUsage{counter: counter},
		},
	}
}

func (b *usageTrackingBody) Read(p []byte) (int, error) {
	n, err := b.body.Read(p)
	if n > 0 {
		b.parser.write(p[:n])
	}
	if err == io.EOF {
		b.finalize()
	}
	return n, err
}

func (b *usageTrackingBody) Close() error {
	b.finalize()
	return b.body.Close()
}

func (b *usageTrackingBody) finalize() {
	if b.finalized {
		return
	}
	b.finalized = true
	b.parser.finalize()
}

type tokenUsageParser struct {
	stream      bool
	usage       responseTokenUsage
	body        []byte
	line        []byte
	lineTooBig  bool
	eventData   []string
	eventBytes  int
	eventTooBig bool
	bodyTooBig  bool
	finalized   bool
}

func (p *tokenUsageParser) write(chunk []byte) {
	if p.stream {
		p.writeStream(chunk)
		return
	}
	if p.bodyTooBig {
		return
	}
	if len(p.body)+len(chunk) > maxTokenUsageBodySize {
		p.body = nil
		p.bodyTooBig = true
		return
	}
	p.body = append(p.body, chunk...)
}

func (p *tokenUsageParser) writeStream(chunk []byte) {
	for len(chunk) > 0 {
		newline := bytes.IndexByte(chunk, '\n')
		if newline == -1 {
			p.appendStreamLine(chunk)
			return
		}
		p.appendStreamLine(chunk[:newline])
		if !p.lineTooBig {
			p.handleStreamLine(string(p.line))
		}
		p.line = p.line[:0]
		p.lineTooBig = false
		chunk = chunk[newline+1:]
	}
}

func (p *tokenUsageParser) appendStreamLine(chunk []byte) {
	if p.lineTooBig {
		return
	}
	if len(p.line)+len(chunk) > maxTokenUsageBodySize {
		p.line = nil
		p.lineTooBig = true
		p.eventTooBig = true
		p.eventData = nil
		p.eventBytes = 0
		return
	}
	p.line = append(p.line, chunk...)
}

func (p *tokenUsageParser) handleStreamLine(line string) {
	line = strings.TrimSuffix(line, "\r")
	if line == "" {
		if p.eventTooBig {
			p.eventTooBig = false
			p.eventData = nil
			p.eventBytes = 0
			return
		}
		p.flushStreamEvent()
		return
	}
	if p.eventTooBig {
		return
	}
	if strings.HasPrefix(line, "data:") {
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if p.eventBytes+len(data)+1 > maxTokenUsageBodySize {
			p.eventTooBig = true
			p.eventData = nil
			p.eventBytes = 0
			return
		}
		p.eventData = append(p.eventData, data)
		p.eventBytes += len(data) + 1
	}
}

func (p *tokenUsageParser) flushStreamEvent() {
	if len(p.eventData) == 0 {
		return
	}
	payload := strings.Join(p.eventData, "\n")
	p.eventData = p.eventData[:0]
	p.eventBytes = 0
	if payload == "[DONE]" {
		return
	}
	p.usage.applyPayload([]byte(payload))
}

func (p *tokenUsageParser) finalize() {
	if p.finalized {
		return
	}
	p.finalized = true
	if p.stream {
		if len(p.line) > 0 && !p.lineTooBig {
			p.handleStreamLine(string(p.line))
		}
		p.line = nil
		p.lineTooBig = false
		if p.eventTooBig {
			p.eventTooBig = false
			p.eventData = nil
			p.eventBytes = 0
			return
		}
		p.flushStreamEvent()
		return
	}
	if p.bodyTooBig || len(p.body) == 0 {
		return
	}
	p.usage.applyPayload(p.body)
}

type apiUsageEnvelope struct {
	Usage *apiUsage `json:"usage"`
}

type apiUsage struct {
	PromptTokens     *int64 `json:"prompt_tokens"`
	CompletionTokens *int64 `json:"completion_tokens"`
	InputTokens      *int64 `json:"input_tokens"`
	OutputTokens     *int64 `json:"output_tokens"`
}

func extractTokenUsage(payload []byte) (uint64, uint64, bool) {
	var envelope apiUsageEnvelope
	if err := json.Unmarshal(payload, &envelope); err != nil || envelope.Usage == nil {
		return 0, 0, false
	}
	upstreamed := firstTokenValue(envelope.Usage.PromptTokens, envelope.Usage.InputTokens)
	downstreamed := firstTokenValue(envelope.Usage.CompletionTokens, envelope.Usage.OutputTokens)
	return upstreamed, downstreamed, upstreamed > 0 || downstreamed > 0
}

func firstTokenValue(values ...*int64) uint64 {
	for _, value := range values {
		if value != nil && *value > 0 {
			return uint64(*value)
		}
	}
	return 0
}
