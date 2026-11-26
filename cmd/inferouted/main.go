// Command inferouted runs the inferoute gateway: an OpenAI-compatible HTTP
// proxy that load-balances and fails over across multiple LLM inference
// backends (e.g. several Ollama or vLLM instances) serving the same model,
// with optional per-key rate limiting and NuclaDB-backed semantic caching.
package main

import (
	"context"
	"flag"
	"log"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"slices"
	"syscall"
	"time"

	"github.com/Rakshit-gen/inferoute/internal/apiauth"
	"github.com/Rakshit-gen/inferoute/internal/cache"
	"github.com/Rakshit-gen/inferoute/internal/config"
	"github.com/Rakshit-gen/inferoute/internal/metrics"
	"github.com/Rakshit-gen/inferoute/internal/proxy"
	"github.com/Rakshit-gen/inferoute/internal/ratelimit"
	"github.com/Rakshit-gen/inferoute/internal/usage"
)

func main() {
	// JSON logs; the standard log package is routed through this too.
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stderr, nil)))

	// Registered first: SIGHUP's default action would kill the process.
	hup := make(chan os.Signal, 1)
	signal.Notify(hup, syscall.SIGHUP)

	configPath := flag.String("config", "config.json", "path to the JSON config file listing backends")
	check := flag.Bool("check", false, "validate the config file and exit")
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatalf("inferouted: %v", err)
	}

	if *check {
		log.Printf("inferouted: %s is valid", *configPath)
		return
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

	h.StreamUsage = cfg.Usage.StreamUsage
	if cfg.Usage.BudgetTokens > 0 {
		window := time.Duration(cfg.Usage.BudgetWindow)
		if cfg.Usage.RedisAddr != "" {
			if h.Budget, err = usage.NewRedis(cfg.Usage.RedisAddr, window); err != nil {
				log.Fatalf("inferouted: %v", err)
			}
		} else {
			h.Budget = usage.NewLocal(window)
		}
		h.BudgetTokens = cfg.Usage.BudgetTokens
		trusted := cfg.TrustedProxies
		h.CallerKey = func(r *http.Request) string { return ratelimit.CallerKey(r, trusted) }
		log.Printf("inferouted: token budget of %d per caller every %s", cfg.Usage.BudgetTokens, window)
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

	metrics.RegisterBackends(func() []metrics.BackendState {
		var out []metrics.BackendState
		for _, s := range h.CurrentPool().Snapshot() {
			out = append(out, metrics.BackendState{Name: s.Name, Up: s.Healthy, Inflight: s.Inflight})
		}
		return out
	})

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
	srv := newServer(withRequestLog(withSecurityHeaders(withCORS(a.routes(), cfg.CORSOrigins)), cfg.TrustedProxies))
	if err := serve(ctx, srv, ln, time.Duration(cfg.ShutdownTimeout), h.Drain); err != nil {
		log.Fatalf("inferouted: %v", err)
	}
	log.Printf("inferouted: stopped")
}

// withCORS lets browsers on allowed origins call the API. A "*" entry permits
// any origin; otherwise the Origin must be listed exactly. Non-browser clients
// send no Origin and are unaffected.
func withCORS(h http.Handler, allowed []string) http.Handler {
	allowAny := slices.Contains(allowed, "*")
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("Vary", "Origin")
		origin := r.Header.Get("Origin")
		if origin == "" || !(allowAny || slices.Contains(allowed, origin)) {
			h.ServeHTTP(w, r)
			return
		}
		w.Header().Set("Access-Control-Allow-Origin", origin)
		w.Header().Set("Access-Control-Expose-Headers", "X-Inferoute-Cache, X-Inferoute-Backend, X-Request-Id, Retry-After")
		if r.Method == http.MethodOptions && r.Header.Get("Access-Control-Request-Method") != "" {
			w.Header().Add("Vary", "Access-Control-Request-Headers")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", r.Header.Get("Access-Control-Request-Headers"))
			w.Header().Set("Access-Control-Max-Age", "600")
			w.WriteHeader(http.StatusNoContent)
			return
		}
		h.ServeHTTP(w, r)
	})
}
