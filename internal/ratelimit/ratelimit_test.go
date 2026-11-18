package ratelimit

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Rakshit-gen/inferoute/internal/apiauth"
)

func TestMiddlewareAllowsThenRejectsBurst(t *testing.T) {
	l := New(1, 2) // 1 req/s refill, burst of 2
	handler := Middleware(l, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := func() *http.Request {
		r := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
		r.Header.Set("Authorization", "Bearer key-a")
		return r
	}

	for i := 0; i < 2; i++ {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req())
		if rec.Code != http.StatusOK {
			t.Fatalf("request %d: expected 200 within burst, got %d", i, rec.Code)
		}
	}

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req())
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429 after burst exhausted, got %d", rec.Code)
	}
}

func TestMiddlewareIsolatesKeys(t *testing.T) {
	l := New(1, 1)
	keys := []string{"key-a", "key-b", "key-c"}
	handler := apiauth.New(keys).Middleware(Middleware(l, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})))

	for _, key := range keys {
		r := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
		r.Header.Set("Authorization", "Bearer "+key)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, r)
		if rec.Code != http.StatusOK {
			t.Fatalf("key %s: expected fresh limiter to allow first request, got %d", key, rec.Code)
		}
	}
}

func TestRejectionCarriesRetryAfter(t *testing.T) {
	h := Middleware(New(0.1, 1), http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	h.ServeHTTP(httptest.NewRecorder(), req)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusTooManyRequests || rec.Header().Get("Retry-After") != "10" {
		t.Fatalf("status = %d, Retry-After = %q", rec.Code, rec.Header().Get("Retry-After"))
	}
}

func TestUnverifiedTokensShareTheIPBucket(t *testing.T) {
	h := Middleware(New(1, 1), http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	for i, token := range []string{"Bearer a", "Bearer b"} {
		req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
		req.Header.Set("Authorization", token)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if i == 1 && rec.Code != http.StatusTooManyRequests {
			t.Fatalf("rotating an unverified token bypassed the limit: %d", rec.Code)
		}
	}
}

func TestIPBucket(t *testing.T) {
	if a, b := ipBucket("2001:db8:1:2::1"), ipBucket("2001:db8:1:2:ffff::9"); a != b {
		t.Fatalf("same /64 got different buckets: %s, %s", a, b)
	}
	if ipBucket("2001:db8:1:2::1") == ipBucket("2001:db8:1:3::1") {
		t.Fatal("different /64s must not share a bucket")
	}
	if got := ipBucket("::ffff:192.0.2.1"); got != "192.0.2.1" {
		t.Fatalf("mapped IPv4 = %q", got)
	}
}
