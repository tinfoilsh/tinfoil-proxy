package main

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"

	log "github.com/sirupsen/logrus"
)

// user_cache_secret provisions the per-user prompt-cache secret defined by the
// secure prompt caching contract. The router derives the request's prefix-cache
// namespace from it: requests carrying the same secret (under the same API
// identity) share cached prompt prefixes, requests carrying different secrets
// cannot observe each other's cache timing. The secret itself is stripped by
// the router and never reaches the model.
//
// Resolution order, mirroring the Tinfoil SDKs:
//
//  1. a non-empty `user_cache_secret` string or non-string value the local
//     client already sent (never overwritten here),
//  2. the --user-cache-secret flag,
//  3. the TINFOIL_USER_CACHE_SECRET environment variable,
//  4. a generated secret persisted at ~/.tinfoil/user_cache_secret (0600),
//     shared with the Tinfoil SDKs on the same machine.
//
// Empty strings are treated as unset at every level: an empty request field
// is replaced, while empty flag and environment values fall through.
//
// Injection happens in the forwarding transport, above the SDK's pinned
// connection, so the field travels inside the sealed channel and is only ever
// visible to the verified enclave.

const (
	// userCacheSecretFlag sets the secret explicitly on the command line. An
	// empty value is treated as unset and falls through to the environment or
	// generated persisted secret.
	userCacheSecretFlag = "user-cache-secret"

	// userCacheSecretField is the router-only request-body field. An absent or
	// empty string is replaced with the resolved proxy secret. Non-empty
	// strings and non-string values remain caller-owned.
	userCacheSecretField = "user_cache_secret"

	// userCacheSecretEnv provisions the secret via the environment. An empty
	// value is treated as unset and falls through to the generated persisted
	// secret.
	userCacheSecretEnv = "TINFOIL_USER_CACHE_SECRET"

	// userCacheSecretFile is the persisted-secret path under the home
	// directory. The Tinfoil SDKs use the same file, so one machine gets one
	// cache namespace across tools.
	userCacheSecretDirName  = ".tinfoil"
	userCacheSecretFileName = "user_cache_secret"

	// maxUserCacheSecretBodySize caps how much of a forwarded body the
	// injector buffers for parsing, mirroring the token-usage rewrite's cap.
	// Anything larger keeps streaming through untouched (tenant-wide caching)
	// instead of ballooning the proxy's memory — unlike the SDKs, the proxy
	// forwards arbitrary local-client bytes, so bodies here are unbounded.
	maxUserCacheSecretBodySize = maxTokenUsageBodySize
)

// userCacheSecretPaths are the OpenAI-compatible endpoints whose bodies carry
// the field. Matched by suffix with no /v1 requirement so custom base URLs
// (path-prefixed proxies or /v1-less roots) still qualify. Other endpoints
// (embeddings, audio, files) are excluded: their engines do not prefix-cache
// and may reject unknown fields.
var userCacheSecretPaths = []string{
	"/chat/completions",
	"/completions",
	"/responses",
}

// resolveUserCacheSecret resolves the proxy-level secret: the explicit flag
// wins when non-empty, then the non-empty environment, then the persisted (or
// generated) secret. Empty flag and environment values are treated as unset.
// An empty result is only possible if secure random generation fails.
func resolveUserCacheSecret(explicit string, explicitSet bool) string {
	if explicitSet && explicit != "" {
		return explicit
	}
	if env, ok := os.LookupEnv(userCacheSecretEnv); ok && env != "" {
		return env
	}
	return loadOrGenerateUserCacheSecret()
}

// newUserCacheSecret returns a fresh 256-bit random secret, hex-encoded.
func newUserCacheSecret() string {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		// Never fall back to a weak secret: no secret means tenant-wide
		// caching, which is safe.
		log.WithError(err).Warn("could not generate a user cache secret; requests stay in the tenant-wide cache namespace")
		return ""
	}
	return hex.EncodeToString(b[:])
}

// ephemeralUserCacheSecret is the process-lifetime fallback for when the
// secret cannot be persisted. An unpersisted secret still isolates this
// proxy's cache namespace, but continuity is lost on restart — like a session
// ID, it silently resets the namespace every deploy — so the fallback warns
// once per process.
var ephemeralUserCacheSecret = sync.OnceValue(func() string {
	secret := newUserCacheSecret()
	if secret != "" {
		log.Warnf("could not persist the user cache secret; using an in-memory secret, so prompt-cache continuity resets when the proxy exits (set %s or --%s to pin one)", userCacheSecretEnv, userCacheSecretFlag)
	}
	return secret
})

// loadOrGenerateUserCacheSecret returns the secret persisted under the user's
// home directory, generating and persisting one on first use. When the home
// directory is unavailable or unwritable it falls back to a process-lifetime
// in-memory secret.
func loadOrGenerateUserCacheSecret() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ephemeralUserCacheSecret()
	}
	dir := filepath.Join(home, userCacheSecretDirName)
	path := filepath.Join(dir, userCacheSecretFileName)

	if b, err := os.ReadFile(path); err == nil {
		if s := strings.TrimSpace(string(b)); s != "" {
			return s
		}
	}

	secret := newUserCacheSecret()
	if secret == "" {
		return ""
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return ephemeralUserCacheSecret()
	}

	// O_EXCL so a concurrent first run loses the race cleanly: the loser
	// re-reads and adopts the winner's secret instead of splitting the
	// machine's namespace between two values.
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	switch {
	case err == nil:
		_, werr := f.WriteString(secret)
		cerr := f.Close()
		if werr == nil && cerr == nil {
			return secret
		}
	case errors.Is(err, fs.ErrExist):
		if b, rerr := os.ReadFile(path); rerr == nil {
			if s := strings.TrimSpace(string(b)); s != "" {
				return s
			}
		}
	default:
		return ephemeralUserCacheSecret()
	}

	// The file exists but is blank or torn: rewrite it in place.
	if err := os.WriteFile(path, []byte(secret), 0o600); err != nil {
		return ephemeralUserCacheSecret()
	}
	return secret
}

// userCacheSecretTransport injects the proxy-level secret into forwarded
// request bodies on the way out. It sits above the reloading upstream and the
// SDK's sealing transport (pinned TLS or EHBP), so the field is added before
// the body is sealed and router-reselection retries replay the injected body.
// A non-empty string or non-string value the local client already sent is
// never overwritten. An explicit empty string is treated as unset and
// replaced with the proxy-level secret.
type userCacheSecretTransport struct {
	secret    string
	transport http.RoundTripper
}

func (t *userCacheSecretTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if t.secret == "" || !userCacheSecretPathEligible(req) {
		return t.transport.RoundTrip(req)
	}
	if req.ContentLength > maxUserCacheSecretBodySize {
		// Too large to parse: forward the stream untouched rather than
		// buffering it.
		return t.transport.RoundTrip(req)
	}

	raw, err := io.ReadAll(io.LimitReader(req.Body, maxUserCacheSecretBodySize+1))
	if err != nil {
		req.Body.Close()
		return nil, err
	}
	if len(raw) > maxUserCacheSecretBodySize {
		// A chunked (or mis-declared) body larger than the cap: stitch the
		// buffered prefix back onto the remaining stream and forward it
		// untouched, so large uploads are never held in memory here.
		out := req.Clone(req.Context())
		out.Body = readCloser{io.MultiReader(bytes.NewReader(raw), req.Body), req.Body}
		return t.transport.RoundTrip(out)
	}
	req.Body.Close()

	newBody, changed := injectUserCacheSecret(raw, t.secret)
	out := req.Clone(req.Context())
	if !changed {
		// Not a JSON object, or the client supplied a caller-owned value:
		// forward the original bytes untouched.
		out.Body = io.NopCloser(bytes.NewReader(raw))
		return t.transport.RoundTrip(out)
	}

	out.Body = io.NopCloser(bytes.NewReader(newBody))
	out.ContentLength = int64(len(newBody))
	out.Header.Set("Content-Length", strconv.Itoa(len(newBody)))
	// Retries below this layer (router reselection, EHBP key rotation) must
	// replay the injected body, not the client's original.
	out.GetBody = func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(newBody)), nil
	}
	return t.transport.RoundTrip(out)
}

// readCloser pairs a stitched-together reader with the closer of the
// underlying client body.
type readCloser struct {
	io.Reader
	io.Closer
}

// userCacheSecretPathEligible reports whether the request can carry the field:
// a POST with a body to one of the supported endpoints.
func userCacheSecretPathEligible(req *http.Request) bool {
	if req.Method != http.MethodPost || req.Body == nil || req.Body == http.NoBody {
		return false
	}
	for _, p := range userCacheSecretPaths {
		if strings.HasSuffix(req.URL.Path, p) {
			return true
		}
	}
	return false
}

// injectUserCacheSecret adds an absent field or replaces an empty string in a
// JSON-object body. Non-empty strings and non-string values remain
// caller-owned. RawMessage preserves number precision across the re-marshal.
// It reports false — forward the original bytes — for non-object bodies,
// trailing data, caller-owned values, or duplicate top-level fields, whose
// re-marshal would otherwise silently collapse caller data.
func injectUserCacheSecret(raw []byte, secret string) ([]byte, bool) {
	if !utf8.Valid(raw) {
		// encoding/json tolerates invalid UTF-8 inside strings but coerces
		// each bad byte to U+FFFD on re-marshal, silently corrupting the
		// client's bytes. RFC 8259 requires UTF-8, so treat such bodies like
		// any other malformed body: forward them untouched.
		return nil, false
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	first, err := dec.Token()
	if err != nil || first != json.Delim('{') {
		return nil, false
	}

	body := make(map[string]json.RawMessage)
	targetIsEmptyString := false
	for dec.More() {
		key, err := dec.Token()
		if err != nil {
			return nil, false
		}
		name, ok := key.(string)
		if !ok {
			return nil, false
		}
		var value json.RawMessage
		if err := dec.Decode(&value); err != nil {
			return nil, false
		}
		if _, duplicate := body[name]; duplicate {
			return nil, false
		}
		body[name] = value
		if name == userCacheSecretField {
			var stringValue *string
			targetIsEmptyString = json.Unmarshal(value, &stringValue) == nil && stringValue != nil && *stringValue == ""
		}
	}
	if last, err := dec.Token(); err != nil || last != json.Delim('}') || !decodeConsumedAll(dec) {
		return nil, false
	}

	if _, present := body[userCacheSecretField]; present && !targetIsEmptyString {
		return nil, false
	}
	encodedSecret, err := json.Marshal(secret)
	if err != nil {
		return nil, false
	}
	body[userCacheSecretField] = encodedSecret
	newBody, err := json.Marshal(body)
	if err != nil {
		return nil, false
	}
	return newBody, true
}

// decodeConsumedAll reports whether dec has nothing left but trailing
// whitespace: a follow-up Token read returns io.EOF only at true end of
// input. dec.More() is not enough here — it reports "no more elements" at a
// trailing '}' or ']', so a malformed body like `{...}}` would be
// re-marshaled without its trailing bytes and a request the router rejects
// would quietly become one it accepts.
func decodeConsumedAll(dec *json.Decoder) bool {
	_, err := dec.Token()
	return err == io.EOF
}
