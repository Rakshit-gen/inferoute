package proxy

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"

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
	if got := rec.Header().Get("X-Inferoute-Backend"); got != "good" {
		t.Fatalf("X-Inferoute-Backend = %q, want %q", got, "good")
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

// The proxy funnels every request for a model at a few backends. If it
// falls back to http.DefaultTransport (2 idle conns per host) that becomes
// connection churn under load, so New must install a transport with a real
// idle pool.
func TestNewUsesPooledTransport(t *testing.T) {
	pool, err := backend.NewPool([]config.Backend{{Name: "b", URL: "http://127.0.0.1:1", Models: []string{"llama3"}}})
	if err != nil {
		t.Fatalf("NewPool: %v", err)
	}
	tr, ok := New(pool).Client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("expected *http.Transport, got %T", New(pool).Client.Transport)
	}
	if tr.MaxIdleConnsPerHost <= http.DefaultTransport.(*http.Transport).MaxIdleConnsPerHost {
		t.Fatalf("MaxIdleConnsPerHost %d is no better than the default", tr.MaxIdleConnsPerHost)
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
	if gotModel != "llama3" {
		t.Fatalf("expected the backend to see the resolved model name %q, got %q", "llama3", gotModel)
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

func testPool(t *testing.T, specs ...config.Backend) *backend.Pool {
	t.Helper()
	p, err := backend.NewPool(specs)
	if err != nil {
		t.Fatalf("NewPool: %v", err)
	}
	return p
}

func upstream(t *testing.T, fn http.HandlerFunc) string {
	t.Helper()
	srv := httptest.NewServer(fn)
	t.Cleanup(srv.Close)
	return srv.URL
}

func post(h http.Handler, body string, headers ...string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

const modelBody = `{"model":"m"}`

func TestRejectsNonPOST(t *testing.T) {
	h := New(testPool(t, config.Backend{Name: "a", URL: "http://a", Models: []string{"m"}}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/chat/completions", nil))
	if rec.Code != http.StatusMethodNotAllowed || rec.Header().Get("Allow") != "POST" {
		t.Fatalf("status = %d, headers = %v", rec.Code, rec.Header())
	}
}

func TestErrorsAreJSON(t *testing.T) {
	h := New(testPool(t, config.Backend{Name: "a", URL: "http://a", Models: []string{"m"}}))
	rec := post(h, `not json`)
	var got struct {
		Error struct{ Message, Type string } `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil || got.Error.Type != "invalid_request_error" {
		t.Fatalf("body = %s, err = %v", rec.Body, err)
	}
}

func TestRejectsOversizedBody(t *testing.T) {
	h := New(testPool(t, config.Backend{Name: "a", URL: "http://a", Models: []string{"m"}}))
	h.MaxBodyBytes = 8
	if rec := post(h, modelBody); rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestForwardsMultimodalMessages(t *testing.T) {
	url := upstream(t, func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, `{"ok":true}`) })
	h := New(testPool(t, config.Backend{Name: "a", URL: url, Models: []string{"m"}}))
	body := `{"model":"m","messages":[{"role":"user","content":[{"type":"text","text":"what is this"},{"type":"image_url","image_url":{"url":"data:x"}}]}]}`
	if rec := post(h, body); rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
}

func TestUnknownModelIs404(t *testing.T) {
	h := New(testPool(t, config.Backend{Name: "a", URL: "http://a", Models: []string{"m"}}))
	if rec := post(h, `{"model":"other"}`); rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestUnknownModelDoesNotCreateMetricSeries(t *testing.T) {
	h := New(testPool(t, config.Backend{Name: "a", URL: "http://a", Models: []string{"m"}}))
	post(h, `{"model":"attacker-chosen-name"}`)
	families, err := prometheus.DefaultGatherer.Gather()
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range families {
		for _, m := range f.GetMetric() {
			for _, l := range m.GetLabel() {
				if l.GetValue() == "attacker-chosen-name" {
					t.Fatalf("%s recorded a client-chosen label", f.GetName())
				}
			}
		}
	}
}

func TestUpstreamHeadersDropHopByHop(t *testing.T) {
	in := http.Header{"Connection": {"close, X-Private"}, "X-Private": {"1"}, "Proxy-Authorization": {"p"}, "X-Keep": {"1"}}
	out := upstreamHeaders(in)
	if out.Get("X-Private") != "" || out.Get("Proxy-Authorization") != "" || out.Get("Connection") != "" || out.Get("X-Keep") != "1" {
		t.Fatalf("headers = %v", out)
	}
}

func TestUpstreamHeadersDropCookies(t *testing.T) {
	out := upstreamHeaders(http.Header{"Cookie": {"s=1"}, "Content-Type": {"application/x-www-form-urlencoded"}})
	if out.Get("Cookie") != "" || out.Get("Content-Type") != "application/json" {
		t.Fatalf("headers = %v", out)
	}
}

func TestBackendCannotSetGatewayHeaders(t *testing.T) {
	url := upstream(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Set-Cookie", "a=b")
		w.Header().Set("X-Inferoute-Cache", "hit")
		w.Header().Set("X-Other", "kept")
	})
	rec := post(New(testPool(t, config.Backend{Name: "a", URL: url, Models: []string{"m"}})), modelBody)
	h := rec.Header()
	if h.Get("Access-Control-Allow-Origin") != "" || h.Get("Set-Cookie") != "" || h.Get("X-Inferoute-Cache") != "" || h.Get("X-Other") != "kept" {
		t.Fatalf("headers = %v", h)
	}
}

func TestKeepsBackendBasePath(t *testing.T) {
	var got string
	url := upstream(t, func(w http.ResponseWriter, r *http.Request) { got = r.URL.Path })
	post(New(testPool(t, config.Backend{Name: "a", URL: url + "/base", PathPrefix: "/openai", Models: []string{"m"}})), modelBody)
	if got != "/base/openai/v1/chat/completions" {
		t.Fatalf("path = %q", got)
	}
}

func TestClientDisconnectLeavesBackendHealthy(t *testing.T) {
	calls := 0
	url := upstream(t, func(w http.ResponseWriter, r *http.Request) { calls++ })
	pool := testPool(t, config.Backend{Name: "a", URL: url, Models: []string{"m"}})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(modelBody)).WithContext(ctx)
	New(pool).ServeHTTP(httptest.NewRecorder(), req)
	if !pool.Snapshot()[0].Healthy || calls != 0 {
		t.Fatalf("healthy = %v, calls = %d", pool.Snapshot()[0].Healthy, calls)
	}
}

func TestEachBackendTriedOnce(t *testing.T) {
	calls := 0
	url := upstream(t, func(w http.ResponseWriter, r *http.Request) { calls++; w.WriteHeader(500) })
	h := New(testPool(t, config.Backend{Name: "a", URL: url, Models: []string{"m"}}))
	post(h, modelBody)
	if calls != 1 {
		t.Fatalf("calls = %d", calls)
	}
}

func TestSingle5xxKeepsBackendInRotation(t *testing.T) {
	url := upstream(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(500) })
	pool := testPool(t, config.Backend{Name: "a", URL: url, Models: []string{"m"}})
	post(New(pool), modelBody)
	if !pool.Snapshot()[0].Healthy {
		t.Fatal("one 5xx must not eject the backend")
	}
}

func Test429FailsOverWithoutEjecting(t *testing.T) {
	a := upstream(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(429) })
	b := upstream(t, func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "ok") })
	pool := testPool(t, config.Backend{Name: "a", URL: a, Models: []string{"m"}}, config.Backend{Name: "b", URL: b, Models: []string{"m"}})
	h := New(pool)
	for i := 0; i < 4; i++ {
		if rec := post(h, modelBody); rec.Code != 200 {
			t.Fatalf("status = %d", rec.Code)
		}
	}
	for _, s := range pool.Snapshot() {
		if !s.Healthy {
			t.Fatalf("%s was ejected by 429s", s.Name)
		}
	}
}

func TestRelaysLastUpstreamError(t *testing.T) {
	url := upstream(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "7")
		w.WriteHeader(503)
		fmt.Fprint(w, `{"error":"loading model"}`)
	})
	rec := post(New(testPool(t, config.Backend{Name: "a", URL: url, Models: []string{"m"}})), modelBody)
	if rec.Code != 503 || rec.Header().Get("Retry-After") != "7" || !strings.Contains(rec.Body.String(), "loading model") {
		t.Fatalf("status = %d, headers = %v, body = %s", rec.Code, rec.Header(), rec.Body)
	}
}

func TestNotImplementedIsNotRetried(t *testing.T) {
	calls := 0
	h1 := func(w http.ResponseWriter, r *http.Request) { calls++; w.WriteHeader(501) }
	a, b := upstream(t, h1), upstream(t, h1)
	if rec := post(New(testPool(t, config.Backend{Name: "a", URL: a, Models: []string{"m"}}, config.Backend{Name: "b", URL: b, Models: []string{"m"}})), modelBody); rec.Code != 501 || calls != 1 {
		t.Fatalf("status = %d, calls = %d", rec.Code, calls)
	}
}

func TestSlowHeadersFailOver(t *testing.T) {
	a := upstream(t, func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body) // lets the server notice the disconnect
		<-r.Context().Done()
	})
	b := upstream(t, func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "ok") })
	h := New(testPool(t, config.Backend{Name: "a", URL: a, Models: []string{"m"}}, config.Backend{Name: "b", URL: b, Models: []string{"m"}}))
	h.UpstreamTimeout = 50 * time.Millisecond
	for i := 0; i < 2; i++ {
		if rec := post(h, modelBody); rec.Code != 200 {
			t.Fatalf("status = %d", rec.Code)
		}
	}
}

func TestLongActiveStreamOutlivesTimeouts(t *testing.T) {
	url := upstream(t, func(w http.ResponseWriter, r *http.Request) {
		for i := 0; i < 5; i++ {
			fmt.Fprint(w, "x")
			w.(http.Flusher).Flush()
			time.Sleep(30 * time.Millisecond)
		}
	})
	h := New(testPool(t, config.Backend{Name: "a", URL: url, Models: []string{"m"}}))
	h.UpstreamTimeout, h.StreamIdleTimeout = 100*time.Millisecond, 100*time.Millisecond
	if rec := post(h, modelBody); rec.Body.String() != "xxxxx" {
		t.Fatalf("body = %q", rec.Body)
	}
}

func TestBrokenStreamAbortsClientConnection(t *testing.T) {
	url := upstream(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "partial")
		w.(http.Flusher).Flush()
		panic(http.ErrAbortHandler)
	})
	gw := httptest.NewServer(New(testPool(t, config.Backend{Name: "a", URL: url, Models: []string{"m"}})))
	defer gw.Close()
	resp, err := http.Post(gw.URL+"/v1/chat/completions", "application/json", strings.NewReader(modelBody))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if _, err := io.ReadAll(resp.Body); err == nil {
		t.Fatal("the client must see the truncated response as an error")
	}
}

func TestShedsLoadAtCapacity(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	url := upstream(t, func(w http.ResponseWriter, r *http.Request) { close(entered); <-release })
	h := New(testPool(t, config.Backend{Name: "a", URL: url, Models: []string{"m"}}))
	h.SetMaxConcurrent(1)
	done := make(chan struct{})
	go func() { post(h, modelBody); close(done) }()
	<-entered
	rec := post(h, modelBody)
	close(release)
	<-done
	if rec.Code != 503 || rec.Header().Get("Retry-After") == "" {
		t.Fatalf("status = %d", rec.Code)
	}
}
