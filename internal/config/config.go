// Package config loads inferoute's JSON configuration file: the set of
// inference backends to route to and how often to health-check them.
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"time"
)

type Backend struct {
	Name   string   `json:"name"`
	URL    string   `json:"url"`
	Models []string `json:"models"`
	// PathPrefix is prepended to the client's request path before
	// forwarding, for backends that mount their API under a path (e.g.
	// Groq's OpenAI-compatible endpoint lives under /openai).
	PathPrefix string `json:"path_prefix"`
	// APIKey, if set, is sent as "Authorization: Bearer <APIKey>" to this
	// backend, overriding whatever the client sent — for backends that need
	// a credential the caller shouldn't have to know (e.g. gatewaying a
	// hosted provider behind a gateway that itself holds the key).
	APIKey string `json:"api_key"`
}

type RateLimit struct {
	Enabled           bool    `json:"enabled"`
	RequestsPerSecond float64 `json:"requests_per_second"`
	Burst             int     `json:"burst"`
	// RedisAddr, if set, shares rate-limit state across gateway instances via
	// Redis instead of the default in-process (single-instance) limiter.
	RedisAddr string `json:"redis_addr"`
}

type Cache struct {
	Enabled              bool    `json:"enabled"`
	NuclaDBAddr          string  `json:"nucladb_addr"`
	EmbeddingBackendAddr string  `json:"embedding_backend_addr"`
	EmbeddingModel       string  `json:"embedding_model"`
	MaxDistance          float32 `json:"max_distance"`
	TenantID             string  `json:"tenant_id"`
}

type Config struct {
	ListenAddr          string        `json:"listen_addr"`
	HealthCheckPath     string        `json:"health_check_path"`
	HealthCheckInterval time.Duration `json:"health_check_interval"`
	Backends            []Backend     `json:"backends"`
	RateLimit           RateLimit     `json:"rate_limit"`
	Cache               Cache         `json:"cache"`
	// ModelAliases maps a requested model name to the model name backends
	// actually serve, e.g. {"gpt-4": "llama3"} routes gpt-4 requests to
	// whatever backend lists "llama3".
	ModelAliases map[string]string `json:"model_aliases"`
	// CORSOrigins is the list of browser origins allowed to call the HTTP
	// API (the dashboard at web/, plus the playground's POST). A single "*"
	// entry allows any origin. Defaults to ["*"].
	CORSOrigins []string `json:"cors_origins"`
	// APIKeys, when non-empty, is the allowlist of bearer tokens accepted on
	// POST /v1/chat/completions — any other request gets 401. Empty (the
	// default) leaves the proxy open, matching inferoute's default posture.
	// Reloaded on SIGHUP alongside backends.
	APIKeys []string `json:"api_keys"`
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config %s: %w", path, err)
	}

	var raw struct {
		ListenAddr          string            `json:"listen_addr"`
		HealthCheckPath     string            `json:"health_check_path"`
		HealthCheckInterval string            `json:"health_check_interval"`
		Backends            []Backend         `json:"backends"`
		RateLimit           RateLimit         `json:"rate_limit"`
		Cache               Cache             `json:"cache"`
		ModelAliases        map[string]string `json:"model_aliases"`
		CORSOrigins         []string          `json:"cors_origins"`
		APIKeys             []string          `json:"api_keys"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parsing config %s: %w", path, err)
	}

	cfg := &Config{
		ListenAddr:      raw.ListenAddr,
		HealthCheckPath: raw.HealthCheckPath,
		Backends:        raw.Backends,
		RateLimit:       raw.RateLimit,
		Cache:           raw.Cache,
		ModelAliases:    raw.ModelAliases,
		CORSOrigins:     raw.CORSOrigins,
		APIKeys:         raw.APIKeys,
	}
	if cfg.ListenAddr == "" {
		cfg.ListenAddr = ":8081"
	}
	if len(cfg.CORSOrigins) == 0 {
		cfg.CORSOrigins = []string{"*"}
	}
	if cfg.HealthCheckPath == "" {
		cfg.HealthCheckPath = "/"
	}
	cfg.HealthCheckInterval = 10 * time.Second
	if raw.HealthCheckInterval != "" {
		d, err := time.ParseDuration(raw.HealthCheckInterval)
		if err != nil {
			return nil, fmt.Errorf("parsing health_check_interval %q: %w", raw.HealthCheckInterval, err)
		}
		cfg.HealthCheckInterval = d
	}
	if len(cfg.Backends) == 0 {
		return nil, fmt.Errorf("config %s: no backends configured", path)
	}
	for _, b := range cfg.Backends {
		if b.Name == "" || b.URL == "" || len(b.Models) == 0 {
			return nil, fmt.Errorf("config %s: backend %+v missing name, url, or models", path, b)
		}
	}

	if cfg.RateLimit.Enabled && cfg.RateLimit.Burst == 0 {
		cfg.RateLimit.Burst = 1
	}
	if cfg.Cache.Enabled {
		if cfg.Cache.NuclaDBAddr == "" || cfg.Cache.EmbeddingBackendAddr == "" || cfg.Cache.EmbeddingModel == "" {
			return nil, fmt.Errorf("config %s: cache.enabled requires nucladb_addr, embedding_backend_addr, and embedding_model", path)
		}
		if cfg.Cache.MaxDistance == 0 {
			cfg.Cache.MaxDistance = 0.05
		}
		if cfg.Cache.TenantID == "" {
			cfg.Cache.TenantID = "inferoute-cache"
		}
	}
	return cfg, nil
}
