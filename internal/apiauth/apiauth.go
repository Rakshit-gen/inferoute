// Package apiauth gates the proxy behind a fixed allowlist of API keys.
// It is deliberately not a user system: when no keys are configured every
// request passes, matching inferoute's default of being an open proxy.
package apiauth

import (
	"crypto/subtle"
	"net/http"
	"strings"
	"sync/atomic"
)

// Gate holds the current allowlist. It is safe for concurrent use and its
// key set can be swapped at runtime (SIGHUP reload).
type Gate struct {
	keys atomic.Pointer[[]string]
}

func New(keys []string) *Gate {
	g := &Gate{}
	g.Set(keys)
	return g
}

// Set replaces the allowlist.
func (g *Gate) Set(keys []string) {
	cp := append([]string(nil), keys...)
	g.keys.Store(&cp)
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
	match := 0
	for _, k := range keys {
		// Compare every key (no early return) so a caller can't learn which
		// key prefix matched from response timing.
		match |= subtle.ConstantTimeCompare([]byte(k), []byte(presented))
	}
	return match == 1
}

// Middleware rejects requests whose bearer token is not in the allowlist
// with 401. It is a pass-through when no keys are configured.
func (g *Gate) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !g.Allowed(bearer(r)) {
			http.Error(w, "unauthorized: a valid API key is required", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func bearer(r *http.Request) string {
	return strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
}
