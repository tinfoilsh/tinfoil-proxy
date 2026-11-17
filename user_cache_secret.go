package main

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"

	log "github.com/sirupsen/logrus"
)

// user_cache_secret resolves the proxy-wide prompt-cache secret defined by
// the secure prompt caching contract. The router derives the request's
// prefix-cache namespace from it: requests carrying the same secret (under the
// same API identity) share cached prompt prefixes, requests carrying different
// secrets cannot observe each other's cache timing.
//
// Resolution order:
//
//  1. the --user-cache-secret flag,
//  2. the TINFOIL_USER_CACHE_SECRET environment variable,
//  3. a generated secret persisted at ~/.tinfoil/user_cache_secret (0600).
//
// Empty flag and environment values are treated as unset and fall through.
//
// The SDK client adds the resolved secret to eligible request bodies beneath
// the proxy, inside the sealed channel, and never overwrites a non-empty
// `user_cache_secret` the local client already sent.

const (
	// userCacheSecretFlag sets the secret explicitly on the command line. An
	// empty value is treated as unset and falls through to the environment or
	// generated persisted secret.
	userCacheSecretFlag = "user-cache-secret"

	// userCacheSecretEnv provisions the secret via the environment. An empty
	// value is treated as unset and falls through to the generated persisted
	// secret.
	userCacheSecretEnv = "TINFOIL_USER_CACHE_SECRET"

	// userCacheSecretFile is the persisted-secret path under the home
	// directory. The Tinfoil SDKs use the same file, so one machine gets one
	// cache namespace across tools.
	userCacheSecretDirName  = ".tinfoil"
	userCacheSecretFileName = "user_cache_secret"
)

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
