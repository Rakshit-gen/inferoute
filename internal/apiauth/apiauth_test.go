package apiauth

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGateAllowed(t *testing.T) {
	tests := []struct {
		name      string
		keys      []string
		presented string
		want      bool
	}{
		{"no keys configured allows anything", nil, "", true},
		{"no keys configured allows a random token", nil, "whatever", true},
		{"configured key matches", []string{"sk-a", "sk-b"}, "sk-b", true},
		{"unknown token rejected", []string{"sk-a"}, "sk-x", false},
		{"empty token rejected when keys configured", []string{"sk-a"}, "", false},
		{"prefix of a key rejected", []string{"sk-abcdef"}, "sk-abc", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := New(tt.keys).Allowed(tt.presented); got != tt.want {
				t.Fatalf("Allowed(%q) with keys %v = %v, want %v", tt.presented, tt.keys, got, tt.want)
			}
		})
	}
}

func TestGateMiddleware(t *testing.T) {
	g := New([]string{"sk-live-1"})
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusTeapot) })
	h := g.Middleware(next)

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("missing key: status = %d, want 401", rec.Code)
	}

	req.Header.Set("Authorization", "Bearer sk-live-1")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusTeapot {
		t.Fatalf("valid key: status = %d, want 418 (passed through)", rec.Code)
	}

	// Reload to a new set; the old key stops working.
	g.Set([]string{"sk-live-2"})
	req.Header.Set("Authorization", "Bearer sk-live-1")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("after reload: status = %d, want 401", rec.Code)
	}
}
