package proxy

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Rakshit-gen/inferoute/internal/backend"
	"github.com/Rakshit-gen/inferoute/internal/cache"
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

func TestServeHTTPAppliesPathPrefixAndAPIKey(t *testing.T) {
	var gotPath, gotAuth string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		fmt.Fprint(w, `{"ok":true}`)
	}))
	defer upstream.Close()

	pool, err := backend.NewPool([]config.Backend{
		{Name: "b", URL: upstream.URL, Models: []string{"llama3"}, PathPrefix: "/openai", APIKey: "server-side-key"},
	})
	if err != nil {
		t.Fatalf("NewPool: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"llama3"}`))
	req.Header.Set("Authorization", "Bearer client-supplied-key")
	rec := httptest.NewRecorder()
	New(pool).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if gotPath != "/openai/v1/chat/completions" {
		t.Fatalf("expected path prefix applied, got %q", gotPath)
	}
	if gotAuth != "Bearer server-side-key" {
		t.Fatalf("expected backend's own api_key to override the client's, got %q", gotAuth)
	}
}

func TestServeHTTPResolvesModelAlias(t *testing.T) {
	var gotModel string
	backendSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Model string `json:"model"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		gotModel = body.Model
		fmt.Fprint(w, `{"ok":true}`)
	}))
	defer backendSrv.Close()

	pool, err := backend.NewPool([]config.Backend{
		{Name: "b", URL: backendSrv.URL, Models: []string{"llama3"}},
	})
	if err != nil {
		t.Fatalf("NewPool: %v", err)
	}

	h := New(pool)
	h.SetAliases(map[string]string{"gpt-4": "llama3"})

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"gpt-4"}`))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if gotModel != "gpt-4" {
		t.Fatalf("expected the backend to still see the original model name %q in the body, got %q", "gpt-4", gotModel)
	}
}

func TestServeHTTPCachesStreamingResponse(t *testing.T) {
	calls := 0
	sse := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher := w.(http.Flusher)
		fmt.Fprint(w, "data: hello\n\n")
		flusher.Flush()
	}))
	defer sse.Close()

	fakeEmbed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"embedding":[0.1,0.2,0.3]}`)
	}))
	defer fakeEmbed.Close()

	var mu sync.Mutex
	stored := map[string]map[string]string{}
	fakeNuclaDB := http.NewServeMux()
	fakeNuclaDB.HandleFunc("POST /v1/vectors", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			ID       string            `json:"id"`
			Metadata map[string]string `json:"metadata"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		mu.Lock()
		stored[body.ID] = body.Metadata
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	})
	fakeNuclaDB.HandleFunc("POST /v1/search", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if len(stored) == 0 {
			json.NewEncoder(w).Encode(map[string]any{"matches": []any{}})
			return
		}
		var last map[string]string
		for _, v := range stored {
			last = v
		}
		json.NewEncoder(w).Encode(map[string]any{
			"matches": []map[string]any{{"score": float32(0.0), "metadata": last}},
		})
	})
	nucladb := httptest.NewServer(fakeNuclaDB)
	defer nucladb.Close()

	pool, err := backend.NewPool([]config.Backend{
		{Name: "sse", URL: sse.URL, Models: []string{"llama3"}},
	})
	if err != nil {
		t.Fatalf("NewPool: %v", err)
	}

	h := New(pool)
	h.Cache = cache.New(cache.Config{
		NuclaDBAddr: nucladb.URL, EmbeddingBackendAddr: fakeEmbed.URL,
		EmbeddingModel: "test-embed", MaxDistance: 0.05, TenantID: "t1",
	})

	body := `{"model":"llama3","stream":true,"messages":[{"role":"user","content":"hi"}]}`

	rec1 := httptest.NewRecorder()
	h.ServeHTTP(rec1, httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body)))
	if rec1.Code != http.StatusOK || !strings.Contains(rec1.Body.String(), "data: hello") {
		t.Fatalf("first request: status = %d, body = %s", rec1.Code, rec1.Body.String())
	}

	// The store happens async; poll briefly for it to land.
	deadline := time.Now().Add(2 * time.Second)
	for {
		mu.Lock()
		n := len(stored)
		mu.Unlock()
		if n > 0 || time.Now().After(deadline) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	mu.Lock()
	n := len(stored)
	mu.Unlock()
	if n == 0 {
		t.Fatal("expected the streamed response to be stored in the cache")
	}

	rec2 := httptest.NewRecorder()
	h.ServeHTTP(rec2, httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body)))
	if rec2.Code != http.StatusOK {
		t.Fatalf("second request: status = %d, body = %s", rec2.Code, rec2.Body.String())
	}
	if rec2.Header().Get("X-Inferoute-Cache") != "hit" {
		t.Fatalf("expected second request to be a cache hit, headers = %v", rec2.Header())
	}
	if rec2.Header().Get("Content-Type") != "text/event-stream" {
		t.Fatalf("expected cached streaming response to keep its content type, got %q", rec2.Header().Get("Content-Type"))
	}
	if !strings.Contains(rec2.Body.String(), "data: hello") {
		t.Fatalf("unexpected cached body: %s", rec2.Body.String())
	}
	if calls != 1 {
		t.Fatalf("expected exactly 1 backend call (second served from cache), got %d", calls)
	}
}
