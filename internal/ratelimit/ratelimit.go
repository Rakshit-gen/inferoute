// Package ratelimit provides an HTTP middleware that token-bucket limits
// requests per API key (falling back to remote IP when no key is present).
package ratelimit

import (
	"net"
	"net/http"
	"strings"
	"sync"

	"golang.org/x/time/rate"
)

type Limiter struct {
	rps   rate.Limit
	burst int

	mu       sync.Mutex
	limiters map[string]*rate.Limiter
}

func New(requestsPerSecond float64, burst int) *Limiter {
	return &Limiter{
		rps:      rate.Limit(requestsPerSecond),
		burst:    burst,
		limiters: make(map[string]*rate.Limiter),
	}
}

func (l *Limiter) forKey(key string) *rate.Limiter {
	l.mu.Lock()
	defer l.mu.Unlock()
	rl, ok := l.limiters[key]
	if !ok {
		rl = rate.NewLimiter(l.rps, l.burst)
		l.limiters[key] = rl
	}
	return rl
}

// Middleware rejects requests over the per-key rate with 429. Callers with
// no Authorization header share a limiter keyed by remote IP.
func (l *Limiter) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !l.forKey(keyFor(r)).Allow() {
			http.Error(w, "rate limit exceeded", http.StatusTooManyRequests)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func keyFor(r *http.Request) string {
	if auth := r.Header.Get("Authorization"); auth != "" {
		return strings.TrimPrefix(auth, "Bearer ")
	}
	if ip, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return ip
	}
	return r.RemoteAddr
}
