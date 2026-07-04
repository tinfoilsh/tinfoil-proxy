package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"github.com/tinfoilsh/tinfoil-go"
)

const (
	httpReadHeaderTimeout = 30 * time.Second
	httpIdleTimeout       = 120 * time.Second
	httpMaxHeaderBytes    = 1 << 20
	handshakeTimeout      = 60 * time.Second
	maxTokenUsageBodySize = 8 << 20
)

type readyMessage struct {
	Event   string `json:"event"`
	Enclave string `json:"enclave"`
	Repo    string `json:"repo"`
	Listen  string `json:"listen"`
}

type tokenStatsMessage struct {
	Event        string `json:"event"`
	Upstreamed   uint64 `json:"upstreamed"`
	Downstreamed uint64 `json:"downstreamed"`
}

type tokenStatsEmitter func(tokenStatsMessage) error

var stdoutMu sync.Mutex

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

func emitReady(enclave, repo, listen string) error {
	msg := readyMessage{
		Event:   "ready",
		Enclave: enclave,
		Repo:    repo,
		Listen:  listen,
	}
	return emitJSONLine(msg)
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

	// TLS pinning keeps response bodies unencrypted at the HTTP layer, so the
	// reverse proxy forwards accurate framing headers.
	opts := []tinfoil.ClientOption{tinfoil.WithTransport(tinfoil.TransportTLS)}
	if enclaveHost != "" || repo != "" {
		opts = append(opts, tinfoil.WithEnclave(enclaveHost), tinfoil.WithRepo(repo))
	}
	tinfoilClient, err := tinfoil.NewClientWithOptions(opts...)
	if err == nil {
		enclaveHost = tinfoilClient.Enclave()
		repo = tinfoilClient.Repo()
	}
	if err != nil {
		log.WithError(err).Error("failed to create HTTP client")
		return err
	}
	log.Debug("secure HTTP client created successfully")

	targetURL, err := url.Parse("https://" + enclaveHost)
	if err != nil {
		log.WithError(err).Error("failed to parse upstream URL")
		return err
	}

	httpClient := tinfoilClient.HTTPClient()
	var tokens *tokenCounter
	if handshake {
		tokens = newTokenCounter(emitTokenStats)
	}

	proxy := httputil.NewSingleHostReverseProxy(targetURL)
	proxy.Transport = withLoggingTransport(log.StandardLogger(), httpClient.Transport, tokens)
	originalDirector := proxy.Director
	proxy.Director = func(req *http.Request) {
		originalDirector(req)
		req.Host = targetURL.Host
	}

	addr := bindAddress()
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		proxy.ServeHTTP(w, r)
	})

	listener, err := net.Listen("tcp", addr)
	if err != nil {
		log.WithError(err).Error("failed to bind listener")
		return err
	}

	if handshake {
		if err := emitReady(enclaveHost, repo, addr); err != nil {
			listener.Close()
			return err
		}
		if err := waitForGoSignal(handshakeTimeout); err != nil {
			listener.Close()
			return err
		}
	}

	log.WithFields(log.Fields{
		"address":      addr,
		"enclave_host": enclaveHost,
	}).Info("starting HTTP proxy server")
	server := &http.Server{
		Addr:              addr,
		Handler:           localOnlyGuard(addr, mux),
		ReadHeaderTimeout: httpReadHeaderTimeout,
		IdleTimeout:       httpIdleTimeout,
		MaxHeaderBytes:    httpMaxHeaderBytes,
	}
	return server.Serve(listener)
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
	allowed := allowedHosts(addr)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := allowed[r.Host]; !ok {
			http.Error(w, "invalid Host header", http.StatusBadRequest)
			return
		}
		if r.Header.Get("Origin") != "" {
			http.Error(w, "cross-origin requests are not allowed", http.StatusForbidden)
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

	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
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
