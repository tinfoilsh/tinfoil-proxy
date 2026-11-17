package main

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
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

func TestUserCacheSecretFlagExplicitEmptyFallsThrough(t *testing.T) {
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
		t.Fatalf("an explicit empty --%s must still be recorded as set", userCacheSecretFlag)
	}

	// Empty values normalize to unset even though Cobra records the flag.
	t.Setenv(userCacheSecretEnv, "from-env")
	if got := resolveUserCacheSecret(userCacheSecret, flag.Changed); got != "from-env" {
		t.Fatalf("expected an explicit empty flag to fall through, got %q", got)
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

	t.Run("explicit empty falls through to environment", func(t *testing.T) {
		t.Setenv(userCacheSecretEnv, "from-env")
		if got := resolveUserCacheSecret("", true); got != "from-env" {
			t.Fatalf("expected the environment secret, got %q", got)
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

	t.Run("environment set but empty falls through to generation", func(t *testing.T) {
		t.Setenv(userCacheSecretEnv, "")
		if got := resolveUserCacheSecret("", false); len(got) != 64 {
			t.Fatalf("expected a generated secret, got %q", got)
		}
		if _, err := os.Stat(filepath.Join(home, userCacheSecretDirName, userCacheSecretFileName)); err != nil {
			t.Fatalf("an empty environment value must fall through to persistence: %v", err)
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

// inboundJSONRequest mimics a request handed to a server handler: unlike
// http.NewRequest, the server never populates GetBody.
func inboundJSONRequest(t *testing.T, url, body string) *http.Request {
	t.Helper()
	req := postJSONRequest(t, url, body)
	req.GetBody = nil
	return req
}

func TestReplayableBodyTransportBuffersSizedBodies(t *testing.T) {
	// Inbound server requests never carry GetBody. The SDK retries a request
	// once after a key rotation only if the body can be re-read, and router
	// reselection replays it the same way.
	for _, raw := range []string{
		`{"model":"m","user_cache_secret":"caller"}`,
		`[1,2,3]`,
		`not json`,
	} {
		t.Run(raw, func(t *testing.T) {
			capture := &captureRoundTripper{}
			transport := &replayableBodyTransport{transport: capture}
			if _, err := transport.RoundTrip(inboundJSONRequest(t, "https://enclave.example.com/v1/embeddings", raw)); err != nil {
				t.Fatal(err)
			}
			if string(capture.body) != raw {
				t.Fatalf("expected the body to pass through byte-identical, got %q", capture.body)
			}
			if capture.req.GetBody == nil {
				t.Fatal("expected a buffered body to be replayable")
			}
			replay, err := capture.req.GetBody()
			if err != nil {
				t.Fatal(err)
			}
			replayed, err := io.ReadAll(replay)
			if err != nil {
				t.Fatal(err)
			}
			if string(replayed) != raw {
				t.Fatalf("expected the replayed body to match the original, got %q", replayed)
			}
		})
	}
}

func TestReplayableBodyTransportStreamsLargeAndUnsizedBodies(t *testing.T) {
	for name, contentLength := range map[string]int64{
		"over the cap": maxReplayableBodySize + 1,
		"unsized":      -1,
	} {
		t.Run(name, func(t *testing.T) {
			capture := &captureRoundTripper{}
			transport := &replayableBodyTransport{transport: capture}
			req := inboundJSONRequest(t, "https://enclave.example.com/v1/files", "upload")
			req.ContentLength = contentLength
			if _, err := transport.RoundTrip(req); err != nil {
				t.Fatal(err)
			}
			if capture.req != req {
				t.Fatal("expected the request to pass through without buffering")
			}
			if string(capture.body) != "upload" {
				t.Fatalf("expected the body to stream through, got %q", capture.body)
			}
		})
	}
}

func TestNewReverseProxyStackOrder(t *testing.T) {
	reloading := newReloadingUpstream(&upstream{host: "router.tinfoil.sh"}, nil)

	// The logging transport wraps the replay buffer, which wraps the reloading
	// upstream: a buffered body survives router-reselection retries.
	proxy := newReverseProxy(reloading, nil)
	lt, ok := proxy.Transport.(*loggingTransport)
	if !ok {
		t.Fatalf("expected the outermost transport to be the logging transport, got %T", proxy.Transport)
	}
	replay, ok := lt.wrapped.(*replayableBodyTransport)
	if !ok {
		t.Fatalf("expected the logging transport to wrap the replay buffer, got %T", lt.wrapped)
	}
	if replay.transport != reloading {
		t.Fatalf("expected the replay buffer to wrap the reloading upstream, got %T", replay.transport)
	}
}
