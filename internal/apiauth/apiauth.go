// Package apiauth gates the proxy behind a fixed allowlist of API keys.
// It is deliberately not a user system: when no keys are configured every
// request passes, matching inferoute's default of being an open proxy.
package apiauth

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"net/http"
	"strings"
	"sync/atomic"

	"github.com/Rakshit-gen/inferoute/internal/apierr"
	"github.com/Rakshit-gen/inferoute/internal/metrics"
)

// Gate holds the current allowlist. It is safe for concurrent use and its
// key set can be swapped at runtime (SIGHUP reload).
type Gate struct {
	keys atomic.Pointer[[][sha256.Size]byte]
}

func New(keys []string) *Gate {
	g := &Gate{}
	g.Set(keys)
	return g
}

// Set replaces the allowlist.
func (g *Gate) Set(keys []string) {
	hashes := make([][sha256.Size]byte, len(keys))
	for i, k := range keys {
		hashes[i] = sha256.Sum256([]byte(k))
	}
	g.keys.Store(&hashes)
}

// Required reports whether any key is configured (i.e. auth is enforced).
func (g *Gate) Required() bool { return len(*g.keys.Load()) > 0 }

// Allowed reports whether presented matches a configured key. With no keys
// configured, everything is allowed.
func (g *Gate) Allowed(presented string) bool {
	keys := *g.keys.Load()
	if len(keys) == 0 {
		return true
	}
	if presented == "" {
		return false
	}
	sum := sha256.Sum256([]byte(presented))
	match := 0
	for i := range keys {
		// Compare every key (no early return) so timing reveals nothing
		// about which key matched.
		match |= subtle.ConstantTimeCompare(keys[i][:], sum[:])
	}
	return match == 1
}

type identityKey struct{}

// Fingerprint returns a stable one-way identifier for a credential, safe to
// use in logs, cache scopes, and Redis keys.
func Fingerprint(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:16])
}

// Identity returns the fingerprint of the key this request was authenticated
// with, or "" when the gate did not verify one.
func Identity(ctx context.Context) string {
	id, _ := ctx.Value(identityKey{}).(string)
	return id
}

// WithIdentity records a verified caller on ctx.
func WithIdentity(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, identityKey{}, id)
}

// Middleware rejects requests whose bearer token is not in the allowlist
// with 401. An accepted key is removed from the request, so it never reaches
// a backend, and replaced by its fingerprint in the context. It is a
// pass-through when no keys are configured.
func (g *Gate) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := bearer(r)
		if !g.Allowed(token) {
			metrics.Rejected.WithLabelValues("unauthorized").Inc()
			w.Header().Set("WWW-Authenticate", "Bearer")
			apierr.Write(w, http.StatusUnauthorized, apierr.Authentication, "invalid_api_key", "a valid API key is required")
			return
		}
		if g.Required() {
			r = r.WithContext(WithIdentity(r.Context(), Fingerprint(token)))
			r.Header.Del("Authorization")
		}
		next.ServeHTTP(w, r)
	})
}

func bearer(r *http.Request) string {
	scheme, token, ok := strings.Cut(r.Header.Get("Authorization"), " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") {
		return ""
	}
	return strings.TrimSpace(token)
}
