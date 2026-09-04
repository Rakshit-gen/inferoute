// Package ratelimit provides an HTTP middleware that token-bucket limits
// requests per API key (falling back to remote IP when no key is present).
package ratelimit

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// Allower reports whether the caller identified by key may proceed. New
// gives an in-process (single-instance) implementation; NewRedis gives one
// that shares state across gateway instances.
type Allower interface {
	Allow(key string) bool
}

// idleEvictAfter is how long a key's limiter survives with no traffic
// before the sweep reclaims it, bounding map growth under IP churn or key
// rotation.
const idleEvictAfter = 30 * time.Minute

type entry struct {
	limiter *rate.Limiter
	seen    time.Time
}

type Limiter struct {
	rps   rate.Limit
	burst int

	mu       sync.Mutex
	limiters map[string]*entry
}

func New(requestsPerSecond float64, burst int) *Limiter {
	l := &Limiter{
		rps:      rate.Limit(requestsPerSecond),
		burst:    burst,
		limiters: make(map[string]*entry),
	}
	go l.sweepLoop()
	return l
}

func (l *Limiter) Allow(key string) bool {
	l.mu.Lock()
	e, ok := l.limiters[key]
	if !ok {
		e = &entry{limiter: rate.NewLimiter(l.rps, l.burst)}
		l.limiters[key] = e
	}
	e.seen = time.Now()
	rl := e.limiter
	l.mu.Unlock()
	return rl.Allow()
}

func (l *Limiter) sweepLoop() {
	ticker := time.NewTicker(idleEvictAfter)
	defer ticker.Stop()
	for range ticker.C {
		cutoff := time.Now().Add(-idleEvictAfter)
		l.mu.Lock()
		for key, e := range l.limiters {
			if e.seen.Before(cutoff) {
				delete(l.limiters, key)
			}
		}
		l.mu.Unlock()
	}
}

// Middleware rejects requests over the per-key rate with 429. Callers with
// no Authorization header share a limiter keyed by remote IP.
func Middleware(l Allower, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !l.Allow(keyFor(r)) {
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
