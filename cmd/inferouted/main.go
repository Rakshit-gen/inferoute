// Command inferouted runs the inferoute gateway: an OpenAI-compatible HTTP
// proxy that load-balances and fails over across multiple LLM inference
// backends (e.g. several Ollama or vLLM instances) serving the same model,
// with optional per-key rate limiting and NuclaDB-backed semantic caching.
package main

import (
	"context"
	"flag"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Rakshit-gen/inferoute/internal/apiauth"
	"github.com/Rakshit-gen/inferoute/internal/cache"
	"github.com/Rakshit-gen/inferoute/internal/config"
	"github.com/Rakshit-gen/inferoute/internal/proxy"
	"github.com/Rakshit-gen/inferoute/internal/ratelimit"
)

func main() {
	// Registered first: SIGHUP's default action would kill the process.
	hup := make(chan os.Signal, 1)
	signal.Notify(hup, syscall.SIGHUP)

	configPath := flag.String("config", "config.json", "path to the JSON config file listing backends")
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatalf("inferouted: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	pool, err := buildPool(ctx, cfg)
	if err != nil {
		log.Fatalf("inferouted: %v", err)
	}

	h := proxy.New(pool)
	h.SetAliases(cfg.ModelAliases)
	h.MaxBodyBytes = cfg.MaxRequestBodyBytes
	h.SetMaxConcurrent(cfg.MaxConcurrentRequests)
	h.UpstreamTimeout = time.Duration(cfg.UpstreamTimeout)
	h.StreamIdleTimeout = time.Duration(cfg.StreamIdleTimeout)

	stopHealthChecks := watchHealth(ctx, pool, cfg)

	if cfg.Cache.Enabled {
		c := cache.New(cache.Config{
			NuclaDBAddr:          cfg.Cache.NuclaDBAddr,
			EmbeddingBackendAddr: cfg.Cache.EmbeddingBackendAddr,
			EmbeddingModel:       cfg.Cache.EmbeddingModel,
			MaxDistance:          cfg.Cache.MaxDistance,
			TenantID:             cfg.Cache.TenantID,
			LookupTimeout:        time.Duration(cfg.Cache.LookupTimeout),
			TTL:                  time.Duration(cfg.Cache.TTL),
		})
		go c.EnsureTenantLoop(ctx, time.Second)
		h.Cache = c
		log.Printf("inferouted: semantic cache enabled against %s (max distance %.2f)", cfg.Cache.NuclaDBAddr, cfg.Cache.MaxDistance)
	}

	var handler http.Handler = h
	if cfg.RateLimit.Enabled {
		var limiter ratelimit.Allower
		if cfg.RateLimit.RedisAddr != "" {
			limiter, err = ratelimit.NewRedis(cfg.RateLimit.RedisAddr, cfg.RateLimit.RequestsPerSecond, cfg.RateLimit.Burst)
			if err != nil {
				log.Fatalf("inferouted: %v", err)
			}
			log.Printf("inferouted: rate limiting enabled at %.1f req/s, burst %d, per API key, shared via redis", cfg.RateLimit.RequestsPerSecond, cfg.RateLimit.Burst)
		} else {
			limiter = ratelimit.New(cfg.RateLimit.RequestsPerSecond, cfg.RateLimit.Burst)
			log.Printf("inferouted: rate limiting enabled at %.1f req/s, burst %d, per API key", cfg.RateLimit.RequestsPerSecond, cfg.RateLimit.Burst)
		}
		handler = ratelimit.MiddlewareBehind(limiter, cfg.TrustedProxies, h)
	}

	gate := apiauth.New(cfg.APIKeys)
	if gate.Required() {
		log.Printf("inferouted: API-key auth enabled (%d key(s))", len(cfg.APIKeys))
	}

	a := &app{h: h, gate: gate, proxied: handler}
	a.cfg.Store(cfg)

	// A SIGHUP reloads backends, aliases, API keys and health settings from
	// configPath without a restart. Rate limit and cache settings are not
	// reloaded.
	go func() {
		for range hup {
			if err := a.reload(ctx, *configPath, &stopHealthChecks); err != nil {
				log.Printf("inferouted: reload: %v (keeping current config)", err)
				continue
			}
			log.Printf("inferouted: reloaded config from %s", *configPath)
		}
	}()

	ln, err := net.Listen("tcp", cfg.ListenAddr)
	if err != nil {
		log.Fatalf("inferouted: %v", err)
	}
	log.Printf("inferouted: listening on %s, routing to %d backend(s)", ln.Addr(), len(cfg.Backends))
	srv := newServer(withCORS(a.routes(), cfg.CORSOrigins))
	if err := serve(ctx, srv, ln, time.Duration(cfg.ShutdownTimeout), h.Drain); err != nil {
		log.Fatalf("inferouted: %v", err)
	}
	log.Printf("inferouted: stopped")
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
