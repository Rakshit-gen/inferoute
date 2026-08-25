package proxy

import (
	"bufio"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Rakshit-gen/inferoute/internal/backend"
	"github.com/Rakshit-gen/inferoute/internal/config"
)

func TestServeHTTPRoutesToCorrectBackend(t *testing.T) {
	good := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"choices":[{"message":{"content":"hi from good"}}]}`)
	}))
	defer good.Close()

	pool, err := backend.NewPool([]config.Backend{
		{Name: "good", URL: good.URL, Models: []string{"llama3"}},
	})
	if err != nil {
		t.Fatalf("NewPool: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"llama3"}`))
	rec := httptest.NewRecorder()
	New(pool).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "hi from good") {
		t.Fatalf("unexpected body: %s", rec.Body.String())
	}
}

func TestServeHTTPFailsOverToHealthyBackend(t *testing.T) {
	down := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer down.Close()
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"ok":true}`)
	}))
	defer up.Close()

	pool, err := backend.NewPool([]config.Backend{
		{Name: "down", URL: down.URL, Models: []string{"llama3"}},
		{Name: "up", URL: up.URL, Models: []string{"llama3"}},
	})
	if err != nil {
		t.Fatalf("NewPool: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"llama3"}`))
	rec := httptest.NewRecorder()
	New(pool).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"ok":true`) {
		t.Fatalf("expected failover to succeed, got status %d body %s", rec.Code, rec.Body.String())
	}
}

func TestServeHTTPStreamsResponse(t *testing.T) {
	sse := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher := w.(http.Flusher)
		for i := 0; i < 3; i++ {
			fmt.Fprintf(w, "data: chunk-%d\n\n", i)
			flusher.Flush()
		}
	}))
	defer sse.Close()

	pool, err := backend.NewPool([]config.Backend{
		{Name: "sse", URL: sse.URL, Models: []string{"llama3"}},
	})
	if err != nil {
		t.Fatalf("NewPool: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"llama3"}`))
	rec := httptest.NewRecorder()
	New(pool).ServeHTTP(rec, req)

	scanner := bufio.NewScanner(io.NopCloser(strings.NewReader(rec.Body.String())))
	var chunks int
	for scanner.Scan() {
		if strings.HasPrefix(scanner.Text(), "data: chunk-") {
			chunks++
		}
	}
	if chunks != 3 {
		t.Fatalf("expected 3 SSE chunks, got %d (body: %s)", chunks, rec.Body.String())
	}
}

func TestServeHTTPRejectsMissingModel(t *testing.T) {
	pool, _ := backend.NewPool([]config.Backend{
		{Name: "a", URL: "http://unused", Models: []string{"llama3"}},
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{}`))
	rec := httptest.NewRecorder()
	New(pool).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing model, got %d", rec.Code)
	}
}
