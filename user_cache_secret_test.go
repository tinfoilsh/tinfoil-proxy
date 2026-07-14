package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/tinfoilsh/tinfoil-go"
)

func TestUserCacheSecretFlagExplicitEmptyCountsAsSet(t *testing.T) {
	flag := rootCmd.Flags().Lookup(userCacheSecretFlag)
	if flag == nil {
		t.Fatalf("expected --%s to be registered", userCacheSecretFlag)
	}
	if flag.Changed {
		t.Fatalf("--%s should start out unset", userCacheSecretFlag)
	}
	t.Cleanup(func() {
		flag.Changed = false
		_ = flag.Value.Set(flag.DefValue)
		userCacheSecret = flag.DefValue
	})

	if err := rootCmd.Flags().Set(userCacheSecretFlag, ""); err != nil {
		t.Fatal(err)
	}
	if !flag.Changed {
		t.Fatalf("an explicit empty --%s must still count as set (it disables provisioning)", userCacheSecretFlag)
	}

	// The explicit empty flag disables provisioning despite the environment.
	t.Setenv(tinfoil.UserCacheSecretEnv, "from-env")
	if got := tinfoil.ResolveUserCacheSecret(userCacheSecret, flag.Changed); got != "" {
		t.Fatalf("expected an explicit empty flag to disable provisioning, got %q", got)
	}
}

// captureRoundTripper records the outgoing request and its body.
type captureRoundTripper struct {
	req  *http.Request
	body []byte
}

func (c *captureRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	c.req = req
	if req.Body != nil && req.Body != http.NoBody {
		body, err := io.ReadAll(req.Body)
		if err != nil {
			return nil, err
		}
		c.body = body
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Status:     "200 OK",
		Body:       io.NopCloser(strings.NewReader("{}")),
		Header:     make(http.Header),
	}, nil
}

func postJSONRequest(t *testing.T, url, body string) *http.Request {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, url, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	return req
}

func TestUserCacheSecretTransportInjects(t *testing.T) {
	paths := []string{
		"/v1/chat/completions",
		"/v1/completions",
		"/v1/responses",
		"/api/v1/chat/completions", // client base URL with a path prefix
		"/chat/completions",        // client base URL without a /v1 root
	}
	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			capture := &captureRoundTripper{}
			transport := &userCacheSecretTransport{secret: "s1", transport: capture}

			resp, err := transport.RoundTrip(postJSONRequest(t, "https://enclave.example.com"+path, `{"model":"m"}`))
			if err != nil {
				t.Fatal(err)
			}
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("expected %d, got %d", http.StatusOK, resp.StatusCode)
			}

			var body map[string]any
			if err := json.Unmarshal(capture.body, &body); err != nil {
				t.Fatal(err)
			}
			if body[tinfoil.UserCacheSecretField] != "s1" {
				t.Fatalf("expected the secret to be injected, got %v", body[tinfoil.UserCacheSecretField])
			}

			// Length metadata and the replayable body must describe the
			// injected bytes: retries below this layer (router reselection)
			// re-send via GetBody.
			if capture.req.ContentLength != int64(len(capture.body)) {
				t.Fatalf("expected content length %d, got %d", len(capture.body), capture.req.ContentLength)
			}
			if got := capture.req.Header.Get("Content-Length"); got != strconv.Itoa(len(capture.body)) {
				t.Fatalf("expected Content-Length header %d, got %q", len(capture.body), got)
			}
			if capture.req.GetBody == nil {
				t.Fatal("expected a replayable body")
			}
			replay, err := capture.req.GetBody()
			if err != nil {
				t.Fatal(err)
			}
			replayed, err := io.ReadAll(replay)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(replayed, capture.body) {
				t.Fatalf("expected the replayed body to match the injected bytes, got %q and %q", replayed, capture.body)
			}
		})
	}
}

func TestUserCacheSecretTransportSkipsIneligibleRequests(t *testing.T) {
	t.Run("non-allowlisted endpoint forwards the body untouched", func(t *testing.T) {
		for _, path := range []string{"/v1/embeddings", "/embeddings"} {
			capture := &captureRoundTripper{}
			transport := &userCacheSecretTransport{secret: "s1", transport: capture}
			const raw = `{"model":"m","input":"text"}`
			if _, err := transport.RoundTrip(postJSONRequest(t, "https://enclave.example.com"+path, raw)); err != nil {
				t.Fatal(err)
			}
			if string(capture.body) != raw {
				t.Fatalf("expected the body to pass through byte-identical for %s, got %q", path, capture.body)
			}
		}
	})

	t.Run("GET with no body is forwarded as-is", func(t *testing.T) {
		capture := &captureRoundTripper{}
		transport := &userCacheSecretTransport{secret: "s1", transport: capture}
		req, err := http.NewRequest(http.MethodGet, "https://enclave.example.com/v1/models", nil)
		if err != nil {
			t.Fatal(err)
		}
		resp, err := transport.RoundTrip(req)
		if err != nil {
			t.Fatal(err)
		}
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected %d, got %d", http.StatusOK, resp.StatusCode)
		}
	})

	t.Run("empty secret disables injection", func(t *testing.T) {
		capture := &captureRoundTripper{}
		transport := &userCacheSecretTransport{secret: "", transport: capture}
		const raw = `{"model":"m"}`
		if _, err := transport.RoundTrip(postJSONRequest(t, "https://enclave.example.com/v1/chat/completions", raw)); err != nil {
			t.Fatal(err)
		}
		if string(capture.body) != raw {
			t.Fatalf("expected the body to pass through byte-identical, got %q", capture.body)
		}
	})
}

func TestUserCacheSecretTransportNeverClobbers(t *testing.T) {
	cases := []struct {
		name string
		raw  string
	}{
		{"explicit per-request secret", `{"model":"m","user_cache_secret":"end-user-7"}`},
		{"explicit empty opt-out", `{"model":"m","user_cache_secret":""}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			capture := &captureRoundTripper{}
			transport := &userCacheSecretTransport{secret: "proxy-level", transport: capture}
			if _, err := transport.RoundTrip(postJSONRequest(t, "https://enclave.example.com/v1/chat/completions", tc.raw)); err != nil {
				t.Fatal(err)
			}
			if string(capture.body) != tc.raw {
				t.Fatalf("a body that already carries the field must pass through byte-identical, got %q", capture.body)
			}
		})
	}
}

func TestUserCacheSecretTransportNonObjectBodies(t *testing.T) {
	// The trailing '}' / ']' cases are the regression: dec.More() reports
	// "no more elements" at either byte, so they used to be re-marshaled
	// with the trailing bytes silently dropped.
	for _, raw := range []string{
		`not json`,
		`[1,2,3]`,
		`null`,
		`{"model":"m"} trailing`,
		`{"model":"m"}}`,
		`{"model":"m"}]`,
		`{"model":"m"}} garbage`,
	} {
		t.Run(raw, func(t *testing.T) {
			capture := &captureRoundTripper{}
			transport := &userCacheSecretTransport{secret: "s1", transport: capture}
			if _, err := transport.RoundTrip(postJSONRequest(t, "https://enclave.example.com/v1/chat/completions", raw)); err != nil {
				t.Fatal(err)
			}
			if string(capture.body) != raw {
				t.Fatalf("bodies the router-side schema would reject must be forwarded untouched, got %q", capture.body)
			}
		})
	}
}

func TestUserCacheSecretTransportSkipsInvalidUTF8(t *testing.T) {
	// encoding/json accepts invalid UTF-8 inside strings but re-marshals each
	// bad byte as U+FFFD, which would silently corrupt the client's message
	// content in transit. Such bodies must pass through byte-identical.
	capture := &captureRoundTripper{}
	transport := &userCacheSecretTransport{secret: "s1", transport: capture}
	raw := "{\"model\":\"m\",\"content\":\"\xff\xfe\"}"
	if _, err := transport.RoundTrip(postJSONRequest(t, "https://enclave.example.com/v1/chat/completions", raw)); err != nil {
		t.Fatal(err)
	}
	if string(capture.body) != raw {
		t.Fatalf("a body with invalid UTF-8 must pass through byte-identical, got %q", capture.body)
	}
}

func TestUserCacheSecretTransportCapsBufferedBodies(t *testing.T) {
	t.Run("declared oversize body streams through untouched", func(t *testing.T) {
		capture := &captureRoundTripper{}
		transport := &userCacheSecretTransport{secret: "s1", transport: capture}
		const raw = `{"model":"m"}`
		req := postJSONRequest(t, "https://enclave.example.com/v1/chat/completions", raw)
		req.ContentLength = maxUserCacheSecretBodySize + 1
		if _, err := transport.RoundTrip(req); err != nil {
			t.Fatal(err)
		}
		if capture.req != req {
			t.Fatal("an oversize body must be forwarded without cloning or buffering")
		}
		if string(capture.body) != raw {
			t.Fatalf("expected the body to pass through byte-identical, got %q", capture.body)
		}
	})

	t.Run("chunked body over the cap forwards byte-identical without injection", func(t *testing.T) {
		capture := &captureRoundTripper{}
		transport := &userCacheSecretTransport{secret: "s1", transport: capture}
		// A valid JSON object bigger than the cap: without the limit this
		// would be parsed and injected; with it, the buffered prefix must be
		// stitched back onto the stream and forwarded untouched.
		raw := `{"model":"m","pad":"` + strings.Repeat("A", maxUserCacheSecretBodySize) + `"}`
		req := postJSONRequest(t, "https://enclave.example.com/v1/chat/completions", raw)
		req.ContentLength = -1 // chunked: length unknown up front
		if _, err := transport.RoundTrip(req); err != nil {
			t.Fatal(err)
		}
		if string(capture.body) != raw {
			t.Fatal("expected the oversize chunked body to pass through byte-identical")
		}
	})

	t.Run("chunked body within the cap still gets injection", func(t *testing.T) {
		capture := &captureRoundTripper{}
		transport := &userCacheSecretTransport{secret: "s1", transport: capture}
		req := postJSONRequest(t, "https://enclave.example.com/v1/chat/completions", `{"model":"m"}`)
		req.ContentLength = -1
		if _, err := transport.RoundTrip(req); err != nil {
			t.Fatal(err)
		}
		var body map[string]any
		if err := json.Unmarshal(capture.body, &body); err != nil {
			t.Fatal(err)
		}
		if body[tinfoil.UserCacheSecretField] != "s1" {
			t.Fatalf("expected the secret to be injected, got %v", body[tinfoil.UserCacheSecretField])
		}
	})
}

func TestUserCacheSecretTransportAllowsTrailingWhitespace(t *testing.T) {
	// Trailing whitespace is not trailing data: strict JSON parsers accept
	// it, so the injection must too — clients routinely end bodies with \n.
	capture := &captureRoundTripper{}
	transport := &userCacheSecretTransport{secret: "s1", transport: capture}
	if _, err := transport.RoundTrip(postJSONRequest(t,
		"https://enclave.example.com/v1/chat/completions", "{\"model\":\"m\"}\n\t ")); err != nil {
		t.Fatal(err)
	}

	var body map[string]any
	if err := json.Unmarshal(capture.body, &body); err != nil {
		t.Fatal(err)
	}
	if body[tinfoil.UserCacheSecretField] != "s1" {
		t.Fatalf("expected the secret to be injected, got %v", body[tinfoil.UserCacheSecretField])
	}
}

func TestUserCacheSecretTransportPreservesNumberPrecision(t *testing.T) {
	// 2^53+1 is not representable as float64; naive decoding would corrupt it.
	capture := &captureRoundTripper{}
	transport := &userCacheSecretTransport{secret: "s1", transport: capture}
	if _, err := transport.RoundTrip(postJSONRequest(t,
		"https://enclave.example.com/v1/chat/completions", `{"model":"m","seed":9007199254740993}`)); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(capture.body), `"seed":9007199254740993`) {
		t.Fatalf("expected the seed to survive injection, got %q", capture.body)
	}
}

func TestNewReverseProxyStackOrder(t *testing.T) {
	reloading := newReloadingUpstream(&upstream{host: "router.tinfoil.sh"}, nil)

	// The logging transport wraps the cache-secret injector, which wraps the
	// reloading upstream: the injected field survives router-reselection
	// retries and is sealed by the pinned connection beneath.
	proxy := newReverseProxy(reloading, "s1", nil)
	lt, ok := proxy.Transport.(*loggingTransport)
	if !ok {
		t.Fatalf("expected the outermost transport to be the logging transport, got %T", proxy.Transport)
	}
	ucs, ok := lt.wrapped.(*userCacheSecretTransport)
	if !ok {
		t.Fatalf("expected the logging transport to wrap the cache-secret injector, got %T", lt.wrapped)
	}
	if ucs.transport != reloading {
		t.Fatalf("expected the injector to wrap the reloading upstream, got %T", ucs.transport)
	}

	// An empty secret (explicit opt-out, or set-but-empty environment) must
	// not add an injection layer at all.
	proxy = newReverseProxy(reloading, "", nil)
	lt, ok = proxy.Transport.(*loggingTransport)
	if !ok {
		t.Fatalf("expected the outermost transport to be the logging transport, got %T", proxy.Transport)
	}
	if lt.wrapped != reloading {
		t.Fatalf("expected no injection layer for an empty secret, got %T", lt.wrapped)
	}
}

// TestUserCacheSecretThroughProxy drives real HTTP requests through the
// proxy's forwarding pipeline (reverse proxy, logging transport, injector,
// reloading upstream), pinning that the proxy-level secret rides forwarded
// bodies exactly as local clients send them — and that a field the client
// already set wins over the proxy-level secret.
func TestUserCacheSecretThroughProxy(t *testing.T) {
	record := &stubUpstreamTransport{}
	reloading := newReloadingUpstream(
		&upstream{host: "router.tinfoil.sh", transport: record},
		func() (*upstream, error) {
			return nil, errors.New("no reload expected")
		},
	)
	server := httptest.NewServer(newReverseProxy(reloading, "proxy-level", nil))
	defer server.Close()

	post := func(body string) {
		t.Helper()
		resp, err := http.Post(server.URL+"/v1/chat/completions", "application/json", strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected %d, got %d", http.StatusOK, resp.StatusCode)
		}
	}

	post(`{"model":"m"}`)
	if len(record.bodies) != 1 {
		t.Fatalf("expected one upstream request, got %d", len(record.bodies))
	}
	var body map[string]any
	if err := json.Unmarshal([]byte(record.bodies[0]), &body); err != nil {
		t.Fatal(err)
	}
	if body[tinfoil.UserCacheSecretField] != "proxy-level" {
		t.Fatalf("expected the proxy-level secret to be injected, got %v", body[tinfoil.UserCacheSecretField])
	}
	if len(record.hosts) != 1 || record.hosts[0] != "router.tinfoil.sh" {
		t.Fatalf("expected the request to target the pinned router, got %v", record.hosts)
	}

	const clientBody = `{"model":"m","user_cache_secret":"end-user-7"}`
	post(clientBody)
	if len(record.bodies) != 2 {
		t.Fatalf("expected two upstream requests, got %d", len(record.bodies))
	}
	if record.bodies[1] != clientBody {
		t.Fatalf("a client-supplied field must pass through byte-identical, got %q", record.bodies[1])
	}
}
