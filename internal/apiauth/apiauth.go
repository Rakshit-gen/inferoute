// Package apiauth gates the proxy behind a fixed allowlist of API keys.
// It is deliberately not a user system: when no keys are configured every
// request passes, matching inferoute's default of being an open proxy.
package apiauth

import (
	"crypto/sha256"
	"crypto/subtle"
	"net/http"
	"strings"
	"sync/atomic"
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
	scheme, token, ok := strings.Cut(r.Header.Get("Authorization"), " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") {
		return ""
	}
	return strings.TrimSpace(token)
}
