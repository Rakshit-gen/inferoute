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
}

type RateLimit struct {
	Enabled           bool    `json:"enabled"`
	RequestsPerSecond float64 `json:"requests_per_second"`
	Burst             int     `json:"burst"`
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
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config %s: %w", path, err)
	}

	var raw struct {
		ListenAddr          string    `json:"listen_addr"`
		HealthCheckPath     string    `json:"health_check_path"`
		HealthCheckInterval string    `json:"health_check_interval"`
		Backends            []Backend `json:"backends"`
		RateLimit           RateLimit `json:"rate_limit"`
		Cache               Cache     `json:"cache"`
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
	}
	if cfg.ListenAddr == "" {
		cfg.ListenAddr = ":8081"
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
