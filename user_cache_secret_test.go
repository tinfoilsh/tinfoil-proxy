package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// unsetUserCacheSecretEnv removes TINFOIL_USER_CACHE_SECRET for the test (the
// developer's shell may set it) and restores it afterwards.
func unsetUserCacheSecretEnv(t *testing.T) {
	t.Helper()
	t.Setenv(userCacheSecretEnv, "placeholder") // registers restoration
	if err := os.Unsetenv(userCacheSecretEnv); err != nil {
		t.Fatal(err)
	}
}

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
	t.Setenv(userCacheSecretEnv, "from-env")
	if got := resolveUserCacheSecret(userCacheSecret, flag.Changed); got != "" {
		t.Fatalf("expected an explicit empty flag to disable provisioning, got %q", got)
	}
}

func TestResolveUserCacheSecretPrecedence(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	t.Run("explicit flag beats environment", func(t *testing.T) {
		t.Setenv(userCacheSecretEnv, "from-env")
		if got := resolveUserCacheSecret("explicit", true); got != "explicit" {
			t.Fatalf("expected the explicit secret to win, got %q", got)
		}
	})

	t.Run("explicit empty disables even with environment set", func(t *testing.T) {
		t.Setenv(userCacheSecretEnv, "from-env")
		if got := resolveUserCacheSecret("", true); got != "" {
			t.Fatalf("expected an empty secret, got %q", got)
		}
	})

	t.Run("environment beats generation and touches no file", func(t *testing.T) {
		t.Setenv(userCacheSecretEnv, "from-env")
		if got := resolveUserCacheSecret("", false); got != "from-env" {
			t.Fatalf("expected the environment secret, got %q", got)
		}
		if _, err := os.Stat(filepath.Join(home, userCacheSecretDirName)); !os.IsNotExist(err) {
			t.Fatalf("an environment-provided secret must not create the secret file, stat err = %v", err)
		}
	})

	t.Run("environment set but empty disables generation", func(t *testing.T) {
		t.Setenv(userCacheSecretEnv, "")
		if got := resolveUserCacheSecret("", false); got != "" {
			t.Fatalf("expected an empty secret, got %q", got)
		}
		if _, err := os.Stat(filepath.Join(home, userCacheSecretDirName)); !os.IsNotExist(err) {
			t.Fatalf("a disabled secret must not create the secret file, stat err = %v", err)
		}
	})
}

func TestUserCacheSecretGenerateAndPersist(t *testing.T) {
	unsetUserCacheSecretEnv(t)
	home := t.TempDir()
	t.Setenv("HOME", home)

	first := resolveUserCacheSecret("", false)
	if len(first) != 64 {
		t.Fatalf("expected a hex-encoded 256-bit secret, got %d chars: %q", len(first), first)
	}

	path := filepath.Join(home, userCacheSecretDirName, userCacheSecretFileName)
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("expected file mode 0600, got %04o", perm)
	}

	// A second resolution (a restart, or another Tinfoil SDK on the same
	// machine) must reuse the persisted secret, not mint a new namespace.
	if second := resolveUserCacheSecret("", false); second != first {
		t.Fatalf("expected the persisted secret to be reused, got %q then %q", first, second)
	}
}

func TestUserCacheSecretAdoptsExistingFile(t *testing.T) {
	unsetUserCacheSecretEnv(t)
	home := t.TempDir()
	t.Setenv("HOME", home)

	dir := filepath.Join(home, userCacheSecretDirName)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	// Trailing newline: the file may be hand-edited or written by another SDK.
	if err := os.WriteFile(filepath.Join(dir, userCacheSecretFileName), []byte("shared-secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if got := resolveUserCacheSecret("", false); got != "shared-secret" {
		t.Fatalf("expected the existing secret to be adopted, got %q", got)
	}
}

func TestUserCacheSecretRewritesBlankFile(t *testing.T) {
	unsetUserCacheSecretEnv(t)
	home := t.TempDir()
	t.Setenv("HOME", home)

	dir := filepath.Join(home, userCacheSecretDirName)
	path := filepath.Join(dir, userCacheSecretFileName)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("  \n"), 0o600); err != nil {
		t.Fatal(err)
	}

	secret := resolveUserCacheSecret("", false)
	if len(secret) != 64 {
		t.Fatalf("expected a fresh 64-char secret, got %q", secret)
	}

	written, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(string(written)); got != secret {
		t.Fatalf("a blank file must be replaced with the generated secret, got %q", got)
	}
}

func TestUserCacheSecretFallsBackWithoutHome(t *testing.T) {
	unsetUserCacheSecretEnv(t)
	t.Setenv("HOME", "")

	first := resolveUserCacheSecret("", false)
	if first == "" {
		t.Fatal("no home directory must still yield a process-lifetime secret")
	}
	if second := resolveUserCacheSecret("", false); second != first {
		t.Fatalf("the in-memory fallback must be stable within the process, got %q then %q", first, second)
	}
}

func TestUserCacheSecretFallsBackWhenHomeNotADirectory(t *testing.T) {
	unsetUserCacheSecretEnv(t)
	homeFile := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(homeFile, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", homeFile)

	if got := resolveUserCacheSecret("", false); got == "" {
		t.Fatal("an unwritable home must still yield a process-lifetime secret")
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
			if body[userCacheSecretField] != "s1" {
				t.Fatalf("expected the secret to be injected, got %v", body[userCacheSecretField])
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
		capture := &captureRoundTripper{}
		transport := &userCacheSecretTransport{secret: "s1", transport: capture}
		const raw = `{"model":"m","input":"text"}`
		if _, err := transport.RoundTrip(postJSONRequest(t, "https://enclave.example.com/v1/embeddings", raw)); err != nil {
			t.Fatal(err)
		}
		if string(capture.body) != raw {
			t.Fatalf("expected the body to pass through byte-identical, got %q", capture.body)
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
	if body[userCacheSecretField] != "s1" {
		t.Fatalf("expected the secret to be injected, got %v", body[userCacheSecretField])
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
	if body[userCacheSecretField] != "proxy-level" {
		t.Fatalf("expected the proxy-level secret to be injected, got %v", body[userCacheSecretField])
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
