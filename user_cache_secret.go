package main

import (
	"bytes"
	"io"
	"net/http"
	"strconv"

	"github.com/tinfoilsh/tinfoil-go"
)

// user_cache_secret provisions the per-user prompt-cache secret defined by the
// secure prompt caching contract. The router derives the request's prefix-cache
// namespace from it: requests carrying the same secret (under the same API
// identity) share cached prompt prefixes, requests carrying different secrets
// cannot observe each other's cache timing. The secret itself is stripped by
// the router and never reaches the model.
//
// Resolution (flag, then TINFOIL_USER_CACHE_SECRET, then the secret persisted
// at ~/.tinfoil/user_cache_secret shared with the Tinfoil SDKs) and the JSON
// injection rules are the SDK's: the proxy calls tinfoil.ResolveUserCacheSecret
// and tinfoil.InjectUserCacheSecret. Only the transport below is proxy-specific,
// because the proxy forwards arbitrary local-client bodies that the SDK's own
// injector does not mutate: they are not replayable in-memory buffers and their
// size is unbounded.
//
// Injection happens in the forwarding transport, above the SDK's pinned
// connection, so the field travels inside the sealed channel and is only ever
// visible to the verified enclave.

const (
	// userCacheSecretFlag sets the secret explicitly on the command line. An
	// explicit empty value disables injection and generation entirely
	// (tenant-wide caching).
	userCacheSecretFlag = "user-cache-secret"

	// maxUserCacheSecretBodySize caps how much of a forwarded body the
	// injector buffers for parsing, mirroring the token-usage rewrite's cap.
	// Anything larger keeps streaming through untouched (tenant-wide caching)
	// instead of ballooning the proxy's memory — unlike the SDKs, the proxy
	// forwards arbitrary local-client bytes, so bodies here are unbounded.
	maxUserCacheSecretBodySize = maxTokenUsageBodySize
)

// userCacheSecretTransport injects the proxy-level secret into forwarded
// request bodies on the way out. It sits above the reloading upstream and the
// SDK's sealing transport (pinned TLS or EHBP), so the field is added before
// the body is sealed and router-reselection retries replay the injected body.
// A field the local client already sent is never overwritten — an explicit
// per-request value, including an explicit empty string (= opt out for that
// request), always wins.
type userCacheSecretTransport struct {
	secret    string
	transport http.RoundTripper
}

func (t *userCacheSecretTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if t.secret == "" || !tinfoil.UserCacheSecretPathEligible(req) {
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

	newBody, changed := tinfoil.InjectUserCacheSecret(raw, t.secret)
	out := req.Clone(req.Context())
	if !changed {
		// Not a JSON object, or the client set the field: forward the
		// original bytes untouched.
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
