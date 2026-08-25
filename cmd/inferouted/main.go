// Command inferouted runs the inferoute gateway: an OpenAI-compatible HTTP
// proxy that load-balances and fails over across multiple LLM inference
// backends (e.g. several Ollama or vLLM instances) serving the same model,
// with optional per-key rate limiting and NuclaDB-backed semantic caching.
package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os/signal"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"

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

	go pool.StartHealthChecks(ctx, cfg.HealthCheckPath, cfg.HealthCheckInterval)

	h := proxy.New(pool)
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
		limiter := ratelimit.New(cfg.RateLimit.RequestsPerSecond, cfg.RateLimit.Burst)
		handler = limiter.Middleware(h)
		log.Printf("inferouted: rate limiting enabled at %.1f req/s, burst %d, per API key", cfg.RateLimit.RequestsPerSecond, cfg.RateLimit.Burst)
	}

	mux := http.NewServeMux()
	mux.Handle("/v1/", handler)
	mux.Handle("/metrics", promhttp.Handler())
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })

	srv := &http.Server{Addr: cfg.ListenAddr, Handler: mux}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()

	log.Printf("inferouted: listening on %s, routing to %d backend(s)", cfg.ListenAddr, len(cfg.Backends))
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("inferouted: %v", err)
	}
}
