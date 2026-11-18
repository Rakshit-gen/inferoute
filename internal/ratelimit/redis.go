package ratelimit

import (
	"context"
	"errors"
	"log"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/redis/go-redis/v9"
)

// tokenBucket refills KEYS[1] at ARGV[1] tokens per second up to ARGV[2] and
// takes one token if available. It uses the Redis clock so gateway instances
// with skewed clocks agree, and expires the key once it would be full again.
var tokenBucket = redis.NewScript(`
local rate = tonumber(ARGV[1])
local burst = tonumber(ARGV[2])
local t = redis.call('TIME')
local now = t[1] * 1000 + math.floor(t[2] / 1000)
local state = redis.call('HMGET', KEYS[1], 't', 'n')
local last = tonumber(state[1]) or now
local tokens = tonumber(state[2]) or burst
tokens = math.min(burst, tokens + math.max(0, now - last) * rate / 1000)
local allowed = 0
if tokens >= 1 then
  tokens = tokens - 1
  allowed = 1
end
redis.call('HSET', KEYS[1], 't', now, 'n', tostring(tokens))
redis.call('PEXPIRE', KEYS[1], math.ceil(burst / rate * 1000) + 1000)
return allowed
`)

// RedisLimiter is a token bucket shared across gateway instances via Redis,
// so a fleet behind a load balancer enforces one limit per key instead of
// one limit per instance.
type RedisLimiter struct {
	rdb   *redis.Client
	rps   float64
	burst int

	local     *Limiter     // enforces the limit per instance during an outage
	downUntil atomic.Int64 // unix nanos before which Redis is not retried
}

const (
	redisCallTimeout = 100 * time.Millisecond
	redisRetryAfter  = time.Second
)

// RetryAfter implements Allower.
func (r *RedisLimiter) RetryAfter() time.Duration { return tokenInterval(r.rps) }

// NewRedis connects to addr, either host:port or a redis:// or rediss:// URL.
func NewRedis(addr string, requestsPerSecond float64, burst int) (*RedisLimiter, error) {
	opts := &redis.Options{Addr: addr}
	if strings.Contains(addr, "://") {
		var err error
		if opts, err = redis.ParseURL(addr); err != nil {
			// The parser's error can quote the URL, which holds the password.
			return nil, errors.New("rate_limit.redis_addr is not a valid redis URL")
		}
	}
	opts.DialTimeout = redisCallTimeout
	opts.ReadTimeout = redisCallTimeout
	opts.WriteTimeout = redisCallTimeout
	opts.MaxRetries = -1 // the fallback is the retry
	return &RedisLimiter{
		rdb:   redis.NewClient(opts),
		rps:   requestsPerSecond,
		burst: max(burst, 1),
		local: New(requestsPerSecond, max(burst, 1)),
	}, nil
}

// Allow falls back to the in-process limiter when Redis is unavailable: an
// outage neither takes the gateway down nor removes the limit.
func (r *RedisLimiter) Allow(key string) bool {
	now := time.Now()
	wasDown := r.downUntil.Load()
	if now.UnixNano() < wasDown {
		return r.local.Allow(key)
	}
	ctx, cancel := context.WithTimeout(context.Background(), redisCallTimeout)
	defer cancel()

	rate := strconv.FormatFloat(r.rps, 'f', -1, 64)
	allowed, err := tokenBucket.Run(ctx, r.rdb, []string{"inferoute:rl:" + key}, rate, r.burst).Int()
	if err != nil {
		if wasDown == 0 {
			log.Printf("inferoute: redis rate limiter unavailable, limiting per instance: %v", err)
		}
		r.downUntil.Store(now.Add(redisRetryAfter).UnixNano())
		return r.local.Allow(key)
	}
	if wasDown != 0 && r.downUntil.CompareAndSwap(wasDown, 0) {
		log.Printf("inferoute: redis rate limiter recovered")
	}
	return allowed == 1
}
