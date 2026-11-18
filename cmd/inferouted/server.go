package main

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"log"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"regexp"
	"sort"
	"sync/atomic"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"

	site "github.com/Rakshit-gen/inferoute/docs/site"
	"github.com/Rakshit-gen/inferoute/internal/apiauth"
	"github.com/Rakshit-gen/inferoute/internal/backend"
	"github.com/Rakshit-gen/inferoute/internal/config"
	"github.com/Rakshit-gen/inferoute/internal/proxy"
	"github.com/Rakshit-gen/inferoute/internal/ratelimit"
)

// app is the running gateway's shared state.
type app struct {
	h    *proxy.Handler
	gate *apiauth.Gate
	cfg  atomic.Pointer[config.Config]

	// proxied is h wrapped with rate limiting, when enabled.
	proxied http.Handler
}

// routes builds the HTTP API. Everything under /v1 and /metrics sits behind
// the API-key gate: they expose backend addresses, models and traffic.
func (a *app) routes() http.Handler {
	v1 := http.NewServeMux()
	v1.Handle("/v1/", a.proxied)
	v1.HandleFunc("GET /v1/backends", a.backends)
	v1.HandleFunc("GET /v1/config", a.config)
	v1.HandleFunc("GET /v1/models", a.models)
	v1.Handle("/metrics", promhttp.Handler())
	gated := a.gate.Middleware(v1)

	mux := http.NewServeMux()
	mux.Handle("/v1/", gated)
	mux.Handle("/metrics", gated)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
		if !a.h.CurrentPool().AnyHealthy() {
			w.WriteHeader(http.StatusServiceUnavailable)
		}
	})
	mux.HandleFunc("GET /docs", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(site.Index)
	})
	return mux
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func (a *app) backends(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, a.h.CurrentPool().Snapshot())
}

func (a *app) config(w http.ResponseWriter, r *http.Request) {
	cfg := a.cfg.Load()
	writeJSON(w, map[string]any{
		"model_aliases":         a.h.Aliases(),
		"load_balancing":        cfg.LoadBalancing,
		"health_check_interval": cfg.HealthCheckInterval.String(),
		"rate_limit": map[string]any{
			"enabled":             cfg.RateLimit.Enabled,
			"requests_per_second": cfg.RateLimit.RequestsPerSecond,
			"burst":               cfg.RateLimit.Burst,
		},
		"cache": map[string]any{
			"enabled":      cfg.Cache.Enabled,
			"max_distance": cfg.Cache.MaxDistance,
		},
		"api_keys_required": a.gate.Required(),
	})
}

// models is the OpenAI-compatible model list: every model a backend serves
// and every alias a client can ask for.
func (a *app) models(w http.ResponseWriter, r *http.Request) {
	ids := map[string]bool{}
	for _, m := range a.h.CurrentPool().Models() {
		ids[m] = true
	}
	for alias := range a.h.Aliases() {
		ids[alias] = true
	}
	sorted := make([]string, 0, len(ids))
	for id := range ids {
		sorted = append(sorted, id)
	}
	sort.Strings(sorted)
	data := make([]map[string]any, 0, len(sorted))
	for _, id := range sorted {
		data = append(data, map[string]any{"id": id, "object": "model", "created": 0, "owned_by": "inferoute"})
	}
	writeJSON(w, map[string]any{"object": "list", "data": data})
}

// buildPool creates a pool for cfg and learns its real health before it
// receives any traffic.
func buildPool(ctx context.Context, cfg *config.Config) (*backend.Pool, error) {
	pool, err := backend.NewPool(cfg.Backends, cfg.LoadBalancing)
	if err != nil {
		return nil, err
	}
	pool.HealthTimeout = time.Duration(cfg.HealthCheckTimeout)
	pool.CheckAll(ctx, cfg.HealthCheckPath)
	return pool, nil
}

// watchHealth runs pool's health loop until the returned func is called.
func watchHealth(ctx context.Context, pool *backend.Pool, cfg *config.Config) context.CancelFunc {
	ctx, cancel := context.WithCancel(ctx)
	go pool.StartHealthChecks(ctx, cfg.HealthCheckPath, time.Duration(cfg.HealthCheckInterval))
	return cancel
}

// reload applies path's backends, aliases, API keys and health settings. The
// running config is untouched if the new one is invalid.
func (a *app) reload(ctx context.Context, path string, stopHealth *context.CancelFunc) error {
	cfg, err := config.Load(path)
	if err != nil {
		return err
	}
	pool, err := buildPool(ctx, cfg)
	if err != nil {
		return err
	}
	(*stopHealth)()
	*stopHealth = watchHealth(ctx, pool, cfg)
	a.h.SetPool(pool)
	a.h.SetAliases(cfg.ModelAliases)
	a.gate.Set(cfg.APIKeys)

	// Settings that need a restart keep describing what is actually running.
	running := *a.cfg.Load()
	running.Backends, running.ModelAliases, running.APIKeys = cfg.Backends, cfg.ModelAliases, cfg.APIKeys
	running.LoadBalancing, running.HealthCheckPath = cfg.LoadBalancing, cfg.HealthCheckPath
	running.HealthCheckInterval, running.HealthCheckTimeout = cfg.HealthCheckInterval, cfg.HealthCheckTimeout
	a.cfg.Store(&running)
	return nil
}

// newServer returns a server hardened against slow clients. There is no
// WriteTimeout or ReadTimeout: generations legitimately run for minutes, and
// the proxy applies its own body and upstream deadlines.
func newServer(handler http.Handler) *http.Server {
	return &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       2 * time.Minute,
		MaxHeaderBytes:    64 << 10,
	}
}

// serve runs srv on ln until ctx ends, then lets in-flight requests finish
// for up to grace before closing what remains. It returns after shutdown.
func serve(ctx context.Context, srv *http.Server, ln net.Listener, grace time.Duration, drain func(context.Context)) error {
	done := make(chan struct{})
	go func() {
		defer close(done)
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), grace)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			log.Printf("inferouted: shutdown: %v; closing remaining connections", err)
			_ = srv.Close()
		}
		drain(shutdownCtx)
	}()
	if err := srv.Serve(ln); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	<-done
	return nil
}

func withSecurityHeaders(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		h.ServeHTTP(w, r)
	})
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) {
	if w.status == 0 {
		w.status = code
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusWriter) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.ResponseWriter.Write(b)
}

func (w *statusWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Unwrap lets http.ResponseController reach the connection.
func (w *statusWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

var validRequestID = regexp.MustCompile(`^[A-Za-z0-9._-]{1,128}$`)

// withRequestLog assigns a request id and writes one access-log line per
// request, including ones aborted by a broken upstream stream.
func withRequestLog(h http.Handler, trusted []netip.Prefix) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Request-Id")
		if !validRequestID.MatchString(id) {
			id = rand.Text()
			r.Header.Set("X-Request-Id", id)
		}
		w.Header().Set("X-Request-Id", id)
		sw := &statusWriter{ResponseWriter: w}
		start := time.Now()
		defer func() {
			switch r.URL.Path {
			case "/healthz", "/readyz", "/metrics":
				return
			}
			slog.Info("request", "id", id, "method", r.Method, "path", r.URL.Path, "status", sw.status,
				"duration_ms", time.Since(start).Milliseconds(), "client", ratelimit.ClientIP(r, trusted),
				"backend", w.Header().Get("X-Inferoute-Backend"))
		}()
		h.ServeHTTP(sw, r)
	})
}
