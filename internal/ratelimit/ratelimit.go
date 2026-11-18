// Package ratelimit provides an HTTP middleware that token-bucket limits
// requests per API key (falling back to remote IP when no key is present).
package ratelimit

import (
	"math"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"

	"github.com/Rakshit-gen/inferoute/internal/apiauth"
	"github.com/Rakshit-gen/inferoute/internal/apierr"
)

// Allower reports whether the caller identified by key may proceed. New
// gives an in-process (single-instance) implementation; NewRedis gives one
// that shares state across gateway instances.
type Allower interface {
	Allow(key string) bool
	// RetryAfter is how long a rejected caller should wait for a new token.
	RetryAfter() time.Duration
}

// tokenInterval is the time one token takes to refill.
func tokenInterval(requestsPerSecond float64) time.Duration {
	return time.Duration(float64(time.Second) / requestsPerSecond)
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

// RetryAfter implements Allower.
func (l *Limiter) RetryAfter() time.Duration { return tokenInterval(float64(l.rps)) }

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

// Middleware rejects requests over the per-key rate with 429. Callers the
// API-key gate verified are limited per key; everyone else per remote IP.
func Middleware(l Allower, next http.Handler) http.Handler {
	return MiddlewareBehind(l, nil, next)
}

// MiddlewareBehind is Middleware for a gateway running behind the proxies in
// trusted, whose X-Forwarded-For entries it believes.
func MiddlewareBehind(l Allower, trusted []netip.Prefix, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !l.Allow(keyFor(r, trusted)) {
			secs := int(math.Ceil(l.RetryAfter().Seconds()))
			w.Header().Set("Retry-After", strconv.Itoa(max(secs, 1)))
			apierr.Write(w, http.StatusTooManyRequests, apierr.RateLimit, "rate_limit_exceeded", "rate limit exceeded")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func keyFor(r *http.Request, trusted []netip.Prefix) string {
	if id := apiauth.Identity(r.Context()); id != "" {
		return "key:" + id
	}
	return "ip:" + ipBucket(ClientIP(r, trusted))
}

// ClientIP returns the caller's address. When the peer is a trusted proxy it
// walks X-Forwarded-For from the right and returns the first address that is
// not itself trusted; entries further left are client-supplied and ignored.
func ClientIP(r *http.Request, trusted []netip.Prefix) string {
	client := remoteIP(r)
	isTrusted := func(ip string) bool {
		addr, err := netip.ParseAddr(ip)
		if err != nil {
			return false
		}
		addr = addr.Unmap().WithZone("")
		for _, p := range trusted {
			if p.Contains(addr) {
				return true
			}
		}
		return false
	}
	if !isTrusted(client) {
		return client
	}
	hops := strings.Split(strings.Join(r.Header.Values("X-Forwarded-For"), ","), ",")
	for i := len(hops) - 1; i >= 0; i-- {
		hop := strings.TrimSpace(hops[i])
		if _, err := netip.ParseAddr(hop); err != nil {
			break
		}
		client = hop
		if !isTrusted(hop) {
			break
		}
	}
	return client
}

func remoteIP(r *http.Request) string {
	if ip, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return ip
	}
	return r.RemoteAddr
}

// ipBucket groups IPv6 addresses by /64, the usual single-customer allocation.
func ipBucket(ip string) string {
	addr, err := netip.ParseAddr(ip)
	if err != nil {
		return ip
	}
	addr = addr.Unmap().WithZone("")
	if addr.Is6() {
		if p, err := addr.Prefix(64); err == nil {
			return p.String()
		}
	}
	return addr.String()
}
