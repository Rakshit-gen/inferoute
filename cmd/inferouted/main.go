// Command inferouted runs the inferoute gateway: an OpenAI-compatible HTTP
// proxy that load-balances and fails over across multiple LLM inference
// backends (e.g. several Ollama or vLLM instances) serving the same model,
// with optional per-key rate limiting and NuclaDB-backed semantic caching.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"

	site "github.com/Rakshit-gen/inferoute/docs/site"
	"github.com/Rakshit-gen/inferoute/internal/backend"
	"github.com/Rakshit-gen/inferoute/internal/cache"
	"github.com/Rakshit-gen/inferoute/internal/config"
	"github.com/Rakshit-gen/inferoute/internal/proxy"
	"github.com/Rakshit-gen/inferoute/internal/ratelimit"
)

func main() {
	configPath := flag.String("config", "config.json", "path to the JSON config file listing backends")
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatalf("inferouted: %v", err)
	}

	pool, err := backend.NewPool(cfg.Backends)
	if err != nil {
		log.Fatalf("inferouted: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	h := proxy.New(pool)
	h.SetAliases(cfg.ModelAliases)

	// startHealthChecks (re)starts the health-check loop for pool and
	// returns a function to stop it — called again on a config reload so
	// the previous pool's loop doesn't leak.
	startHealthChecks := func(p *backend.Pool) context.CancelFunc {
		hctx, cancel := context.WithCancel(ctx)
		go p.StartHealthChecks(hctx, cfg.HealthCheckPath, cfg.HealthCheckInterval)
		return cancel
	}
	stopHealthChecks := startHealthChecks(pool)

	if cfg.Cache.Enabled {
		c := cache.New(cache.Config{
			NuclaDBAddr:          cfg.Cache.NuclaDBAddr,
			EmbeddingBackendAddr: cfg.Cache.EmbeddingBackendAddr,
			EmbeddingModel:       cfg.Cache.EmbeddingModel,
			MaxDistance:          cfg.Cache.MaxDistance,
			TenantID:             cfg.Cache.TenantID,
		})
		if err := c.EnsureTenant(ctx); err != nil {
			log.Printf("inferouted: cache tenant setup: %v (continuing; cache calls will fail until this is fixed)", err)
		}
		h.Cache = c
		log.Printf("inferouted: semantic cache enabled against %s (max distance %.2f)", cfg.Cache.NuclaDBAddr, cfg.Cache.MaxDistance)
	}

	var handler http.Handler = h
	if cfg.RateLimit.Enabled {
		var limiter ratelimit.Allower
		if cfg.RateLimit.RedisAddr != "" {
			limiter = ratelimit.NewRedis(cfg.RateLimit.RedisAddr, cfg.RateLimit.RequestsPerSecond, cfg.RateLimit.Burst)
			log.Printf("inferouted: rate limiting enabled at %.1f req/s, burst %d, per API key, shared via redis at %s", cfg.RateLimit.RequestsPerSecond, cfg.RateLimit.Burst, cfg.RateLimit.RedisAddr)
		} else {
			limiter = ratelimit.New(cfg.RateLimit.RequestsPerSecond, cfg.RateLimit.Burst)
			log.Printf("inferouted: rate limiting enabled at %.1f req/s, burst %d, per API key", cfg.RateLimit.RequestsPerSecond, cfg.RateLimit.Burst)
		}
		handler = ratelimit.Middleware(limiter, h)
	}

	mux := http.NewServeMux()
	mux.Handle("/v1/", handler)
	mux.HandleFunc("/v1/backends", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(h.CurrentPool().Snapshot())
	})
	mux.Handle("/metrics", promhttp.Handler())
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	mux.HandleFunc("GET /docs", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(site.Index)
	})

	srv := &http.Server{Addr: cfg.ListenAddr, Handler: withCORS(mux, cfg.CORSOrigins)}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()

	// A SIGHUP reloads the backend list from configPath without restarting
	// the process. Rate limit and cache settings are not reloaded — only
	// the routing table.
	hup := make(chan os.Signal, 1)
	signal.Notify(hup, syscall.SIGHUP)
	go func() {
		for range hup {
			newCfg, err := config.Load(*configPath)
			if err != nil {
				log.Printf("inferouted: reload: %v (keeping current config)", err)
				continue
			}
			newPool, err := backend.NewPool(newCfg.Backends)
			if err != nil {
				log.Printf("inferouted: reload: %v (keeping current config)", err)
				continue
			}
			stopHealthChecks()
			stopHealthChecks = startHealthChecks(newPool)
			h.SetPool(newPool)
			h.SetAliases(newCfg.ModelAliases)
			log.Printf("inferouted: reloaded config from %s, now routing to %d backend(s)", *configPath, len(newCfg.Backends))
		}
	}()

	log.Printf("inferouted: listening on %s, routing to %d backend(s)", cfg.ListenAddr, len(cfg.Backends))
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("inferouted: %v", err)
	}
}

// withCORS lets browser dashboards on other origins call the read-only
// endpoints and the playground POST /v1/chat/completions. A "*" entry in
// allowed permits any origin; otherwise the request's Origin must be listed
// exactly. Non-browser clients (curl, SDKs) send no Origin and are
// unaffected.
func withCORS(h http.Handler, allowed []string) http.Handler {
	allowAny := false
	set := make(map[string]bool, len(allowed))
	for _, o := range allowed {
		if o == "*" {
			allowAny = true
		}
		set[o] = true
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if origin := r.Header.Get("Origin"); origin != "" && (allowAny || set[origin]) {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Add("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
			w.Header().Set("Access-Control-Expose-Headers", "X-Inferoute-Cache, X-Inferoute-Backend")
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		h.ServeHTTP(w, r)
	})
}
