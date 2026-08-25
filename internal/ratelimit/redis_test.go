package ratelimit

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

// requireRedis skips the test if no Redis is reachable at localhost:6379 —
// this test exercises the real client against a real server rather than
// mocking Redis, but that server isn't assumed to be present everywhere
// (e.g. CI), so it degrades to a skip instead of a failure.
func requireRedis(t *testing.T) {
	t.Helper()
	rdb := redis.NewClient(&redis.Options{Addr: "localhost:6379"})
	defer rdb.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	if err := rdb.Ping(ctx).Err(); err != nil {
		t.Skipf("no redis reachable at localhost:6379: %v", err)
	}
}

func TestRedisLimiterAllowsThenRejectsBurst(t *testing.T) {
	requireRedis(t)
	key := "test-key-" + time.Now().Format(time.RFC3339Nano)

	l := NewRedis("localhost:6379", 1, 2)
	handler := Middleware(l, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := func() *http.Request {
		r := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
		r.Header.Set("Authorization", "Bearer "+key)
		return r
	}

	for i := 0; i < 2; i++ {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req())
		if rec.Code != http.StatusOK {
			t.Fatalf("request %d: expected 200 within limit, got %d", i, rec.Code)
		}
	}

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req())
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429 after limit exhausted, got %d", rec.Code)
	}
}
