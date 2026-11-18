package ratelimit

import (
	"context"
	"strconv"
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
}

// RetryAfter implements Allower.
func (r *RedisLimiter) RetryAfter() time.Duration { return tokenInterval(r.rps) }

func NewRedis(addr string, requestsPerSecond float64, burst int) *RedisLimiter {
	return &RedisLimiter{
		rdb:   redis.NewClient(&redis.Options{Addr: addr}),
		rps:   requestsPerSecond,
		burst: max(burst, 1),
	}
}

// Allow fails open on Redis errors: an unreachable rate-limit store
// shouldn't take the gateway down with it.
func (r *RedisLimiter) Allow(key string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	rate := strconv.FormatFloat(r.rps, 'f', -1, 64)
	allowed, err := tokenBucket.Run(ctx, r.rdb, []string{"inferoute:rl:" + key}, rate, r.burst).Int()
	if err != nil {
		return true
	}
	return allowed == 1
}
