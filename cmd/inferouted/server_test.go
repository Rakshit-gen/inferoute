package main

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Rakshit-gen/inferoute/internal/apiauth"
	"github.com/Rakshit-gen/inferoute/internal/backend"
	"github.com/Rakshit-gen/inferoute/internal/config"
	"github.com/Rakshit-gen/inferoute/internal/proxy"
)

func testApp(t *testing.T, keys ...string) *app {
	t.Helper()
	cfg := &config.Config{
		LoadBalancing: "round_robin",
		Backends:      []config.Backend{{Name: "a", URL: "http://a:1", Models: []string{"m"}}},
		APIKeys:       keys,
	}
	pool, err := backend.NewPool(cfg.Backends)
	if err != nil {
		t.Fatal(err)
	}
	h := proxy.New(pool)
	a := &app{h: h, gate: apiauth.New(keys), proxied: h}
	a.cfg.Store(cfg)
	return a
}

func get(h http.Handler, path string, headers ...string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestIntrospectionRequiresKey(t *testing.T) {
	h := testApp(t, "k").routes()
	for _, path := range []string{"/v1/backends", "/v1/config", "/v1/models", "/metrics"} {
		if rec := get(h, path); rec.Code != http.StatusUnauthorized {
			t.Errorf("%s without a key: %d", path, rec.Code)
		}
		if rec := get(h, path, "Authorization", "Bearer k"); rec.Code != http.StatusOK {
			t.Errorf("%s with a key: %d", path, rec.Code)
		}
	}
	for _, path := range []string{"/healthz", "/docs"} {
		if rec := get(h, path); rec.Code != http.StatusOK {
			t.Errorf("%s must stay public: %d", path, rec.Code)
		}
	}
}

func TestServeWaitsForInFlightRequests(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{})
	srv := newServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		time.Sleep(150 * time.Millisecond)
		io.WriteString(w, "finished")
	}))
	ctx, cancel := context.WithCancel(context.Background())
	drained := false
	result := make(chan error, 1)
	go func() { result <- serve(ctx, srv, ln, 5*time.Second, func(context.Context) { drained = true }) }()

	body := make(chan string, 1)
	go func() {
		resp, err := http.Get("http://" + ln.Addr().String())
		if err != nil {
			body <- err.Error()
			return
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		body <- string(b)
	}()
	<-started
	cancel()
	if err := <-result; err != nil {
		t.Fatal(err)
	}
	if got := <-body; got != "finished" || !drained {
		t.Fatalf("body = %q, drained = %v", got, drained)
	}
}

func TestServerHasSlowClientLimits(t *testing.T) {
	srv := newServer(http.NotFoundHandler())
	if srv.ReadHeaderTimeout == 0 || srv.IdleTimeout == 0 || srv.MaxHeaderBytes == 0 {
		t.Fatalf("missing limits: %+v", srv)
	}
	if srv.WriteTimeout != 0 || srv.ReadTimeout != 0 {
		t.Fatal("whole-request timeouts would cut off long generations")
	}
}

func TestReloadAppliesNewConfig(t *testing.T) {
	probed := make(chan string, 8)
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case probed <- r.URL.Path:
		default:
		}
	}))
	defer up.Close()
	path := filepath.Join(t.TempDir(), "c.json")
	body := `{"health_check_path":"/ready","load_balancing":"weighted","api_keys":["new"],"backends":[{"name":"b","url":"` + up.URL + `","models":["n"]}]}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	a := testApp(t, "old")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stop := context.CancelFunc(func() {})
	if err := a.reload(ctx, path, &stop); err != nil {
		t.Fatal(err)
	}
	defer stop()
	if got := <-probed; got != "/ready" {
		t.Fatalf("probe path = %q", got)
	}
	rec := get(a.routes(), "/v1/config", "Authorization", "Bearer new")
	var out struct {
		LoadBalancing string `json:"load_balancing"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil || out.LoadBalancing != "weighted" {
		t.Fatalf("config = %s", rec.Body)
	}
}

func TestReloadKeepsRunningConfigOnError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "c.json")
	os.WriteFile(path, []byte(`{`), 0o600)
	a := testApp(t, "k")
	stop := context.CancelFunc(func() { t.Fatal("health checks must keep running") })
	if err := a.reload(context.Background(), path, &stop); err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(get(a.routes(), "/v1/models", "Authorization", "Bearer k").Body.String(), `"m"`) {
		t.Fatal("the old pool must stay active")
	}
}

func TestBackendsIsReadOnly(t *testing.T) {
	req := httptest.NewRequest(http.MethodDelete, "/v1/backends", nil)
	rec := httptest.NewRecorder()
	testApp(t).routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestReadyzTracksBackends(t *testing.T) {
	a := testApp(t)
	if rec := get(a.routes(), "/readyz"); rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	pool := a.h.CurrentPool()
	b, release, _ := pool.Next("m")
	release()
	pool.MarkUnhealthy(b)
	if rec := get(a.routes(), "/readyz"); rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d", rec.Code)
	}
}
