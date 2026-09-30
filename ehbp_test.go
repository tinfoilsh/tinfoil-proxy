package main

import (
	"bufio"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	ehbpclient "github.com/tinfoilsh/encrypted-http-body-protocol/client"
	ehbpidentity "github.com/tinfoilsh/encrypted-http-body-protocol/identity"
)

// ehbpEnclave is an EHBP-terminating upstream: a replica as the proxy sees it.
type ehbpEnclave struct {
	server   *httptest.Server
	identity *ehbpidentity.Identity
}

// newEHBPEnclave starts a server whose handler sits behind the EHBP
// middleware. Responses are buffered and re-framed with an explicit
// Content-Length describing the encrypted body, as buffering middleboxes
// between the enclave and the proxy do; this is the framing that used to
// truncate non-streaming replies once decrypted.
func newEHBPEnclave(t *testing.T, handler http.Handler) *ehbpEnclave {
	t.Helper()
	identity, err := ehbpidentity.NewIdentity()
	if err != nil {
		t.Fatal(err)
	}
	encrypted := identity.Middleware()(handler)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Accept") == "text/event-stream" {
			encrypted.ServeHTTP(w, r)
			return
		}
		rec := httptest.NewRecorder()
		encrypted.ServeHTTP(rec, r)
		for key, values := range rec.Header() {
			// The middleware announces chunked framing; a buffering
			// middlebox replaces that with a fixed length.
			if http.CanonicalHeaderKey(key) == "Transfer-Encoding" {
				continue
			}
			for _, value := range values {
				w.Header().Add(key, value)
			}
		}
		w.Header().Set("Content-Length", strconv.Itoa(rec.Body.Len()))
		w.WriteHeader(rec.Code)
		_, _ = w.Write(rec.Body.Bytes())
	}))
	t.Cleanup(server.Close)
	return &ehbpEnclave{server: server, identity: identity}
}

func (e *ehbpEnclave) host() string {
	return strings.TrimPrefix(e.server.URL, "http://")
}

// transport returns an EHBP client transport sealed to the enclave's HPKE key,
// speaking plain HTTP to the test server.
func (e *ehbpEnclave) transport(t *testing.T) http.RoundTripper {
	t.Helper()
	public, err := ehbpidentity.FromPublicKeyHex(e.identity.MarshalPublicKeyHex())
	if err != nil {
		t.Fatal(err)
	}
	transport, err := ehbpclient.NewTransportWithIdentity(public)
	if err != nil {
		t.Fatal(err)
	}
	return &plainHTTPTransport{transport: transport}
}

// plainHTTPTransport rewrites the https scheme the reverse proxy Director sets
// to http so requests reach the httptest server.
type plainHTTPTransport struct {
	transport http.RoundTripper
}

func (p *plainHTTPTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	req.URL.Scheme = "http"
	return p.transport.RoundTrip(req)
}

// startProxy serves the proxy's reverse proxy pipeline over the given
// upstream, mirroring newReverseProxy's wiring in runProxy.
func startProxy(t *testing.T, enclave *ehbpEnclave) *httptest.Server {
	t.Helper()
	proxy := newReverseProxy(enclave.transport(t), enclave.host(), nil, nil)
	server := httptest.NewServer(proxy)
	t.Cleanup(server.Close)
	return server
}

func TestEHBPProxyForwardsFullNonStreamingResponse(t *testing.T) {
	const reply = `{"id":"chatcmpl-1","choices":[{"message":{"content":"Hello from the enclave, this reply must arrive intact"}}]}`
	var received string
	enclave := newEHBPEnclave(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		received = string(body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(reply))
	}))
	proxy := startProxy(t, enclave)

	const request = `{"model":"gpt-oss-120b","messages":[{"role":"user","content":"hi"}]}`
	resp, err := http.Post(proxy.URL+"/v1/chat/completions", "application/json", strings.NewReader(request))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("reading the proxied body: %v", err)
	}
	if string(body) != reply {
		t.Fatalf("expected the full decrypted reply, got %q", body)
	}
	if received != request {
		t.Fatalf("enclave received %q, want %q", received, request)
	}
}

func TestEHBPProxyStreamsSSE(t *testing.T) {
	events := []string{
		`{"choices":[{"delta":{"content":"Hel"}}]}`,
		`{"choices":[{"delta":{"content":"lo"}}]}`,
		`{"choices":[],"usage":{"prompt_tokens":1,"completion_tokens":2}}`,
	}
	enclave := newEHBPEnclave(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)
		for _, event := range events {
			fmt.Fprintf(w, "data: %s\n\n", event)
			flusher.Flush()
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
		flusher.Flush()
	}))
	proxy := startProxy(t, enclave)

	req, err := http.NewRequest(http.MethodPost, proxy.URL+"/v1/chat/completions",
		strings.NewReader(`{"model":"gpt-oss-120b","stream":true}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if got := resp.Header.Get("Content-Length"); got != "" {
		t.Fatalf("a streamed response must not carry a Content-Length, got %q", got)
	}

	var got []string
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		got = append(got, strings.TrimPrefix(line, "data: "))
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("reading the SSE stream: %v", err)
	}
	want := append(append([]string{}, events...), "[DONE]")
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("expected events %q, got %q", want, got)
	}
}
