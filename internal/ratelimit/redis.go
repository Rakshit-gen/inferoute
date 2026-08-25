package ratelimit

import (
	"context"
	"time"

	"github.com/redis/go-redis/v9"
)

// RedisLimiter is a fixed-window request counter shared across gateway
// instances via Redis, so a fleet behind a load balancer enforces one limit
// per key instead of one limit per instance.
//
// ponytail: fixed window per second, not a sliding token bucket, so a caller
// can burst up to 2x the limit across a window boundary. Upgrade to a Lua
// token-bucket script if that matters.
type RedisLimiter struct {
	rdb   *redis.Client
	limit int64
}

func NewRedis(addr string, requestsPerSecond float64, burst int) *RedisLimiter {
	limit := int64(requestsPerSecond)
	if int64(burst) > limit {
		limit = int64(burst)
	}
	if limit < 1 {
		limit = 1
	}
	return &RedisLimiter{
		rdb:   redis.NewClient(&redis.Options{Addr: addr}),
		limit: limit,
	}
}

// Allow fails open on Redis errors: an unreachable rate-limit store
// shouldn't take the gateway down with it.
func (r *RedisLimiter) Allow(key string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	n, err := r.rdb.Incr(ctx, "inferoute:rl:"+key).Result()
	if err != nil {
		return true
	}
	if n == 1 {
		r.rdb.Expire(ctx, "inferoute:rl:"+key, time.Second)
	}
	return n <= r.limit
}
