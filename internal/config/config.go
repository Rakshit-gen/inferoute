// Package config loads inferoute's JSON configuration file: the set of
// inference backends to route to and how often to health-check them.
package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"regexp"
	"strings"
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
	// Weight biases the "weighted" load-balancing strategy toward beefier
	// backends. Ignored by the other strategies. Missing or <=0 means 1.
	Weight int `json:"weight"`
}

// Duration is a time.Duration written in config as a string such as "10s".
type Duration time.Duration

func (d *Duration) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return fmt.Errorf("duration must be a string like \"10s\": %w", err)
	}
	v, err := time.ParseDuration(s)
	if err != nil {
		return err
	}
	*d = Duration(v)
	return nil
}

func (d Duration) String() string { return time.Duration(d).String() }

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
	ListenAddr          string    `json:"listen_addr"`
	HealthCheckPath     string    `json:"health_check_path"`
	HealthCheckInterval Duration  `json:"health_check_interval"`
	Backends            []Backend `json:"backends"`
	RateLimit           RateLimit `json:"rate_limit"`
	Cache               Cache     `json:"cache"`
	// LoadBalancing picks which backend serves each request among the
	// healthy ones for a model: "round_robin" (default, even rotation),
	// "least_pending" (fewest in-flight requests), or "weighted" (random,
	// biased by each backend's Weight).
	LoadBalancing string `json:"load_balancing"`
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

// checkHTTPURL requires an absolute http(s) URL. Its errors never quote the
// URL, which may contain credentials.
func checkHTTPURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return errors.New("not a valid URL")
	}
	if (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return errors.New("must look like http://host:port")
	}
	return nil
}

var envRef = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}`)

// expandSecret replaces each ${VAR} in s with that environment variable.
func expandSecret(s string) (string, error) {
	var missing string
	out := envRef.ReplaceAllStringFunc(s, func(ref string) string {
		name := envRef.FindStringSubmatch(ref)[1]
		v, ok := os.LookupEnv(name)
		if !ok || v == "" {
			missing = name
		}
		return v
	})
	if missing != "" {
		return "", fmt.Errorf("environment variable %s is not set", missing)
	}
	return out, nil
}

// expandSecrets resolves ${VAR} in the fields that hold credentials.
func (c *Config) expandSecrets() (err error) {
	for i := range c.Backends {
		if c.Backends[i].APIKey, err = expandSecret(c.Backends[i].APIKey); err != nil {
			return fmt.Errorf("backend %s api_key: %w", c.Backends[i].Name, err)
		}
	}
	for i := range c.APIKeys {
		if c.APIKeys[i], err = expandSecret(c.APIKeys[i]); err != nil {
			return fmt.Errorf("api_keys[%d]: %w", i, err)
		}
	}
	if c.RateLimit.RedisAddr, err = expandSecret(c.RateLimit.RedisAddr); err != nil {
		return fmt.Errorf("rate_limit.redis_addr: %w", err)
	}
	return nil
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config %s: %w", path, err)
	}

	cfg := &Config{HealthCheckInterval: Duration(10 * time.Second)}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(cfg); err != nil {
		return nil, fmt.Errorf("parsing config %s: %w", path, err)
	}
	if dec.More() {
		return nil, fmt.Errorf("parsing config %s: unexpected data after the JSON object", path)
	}
	if err := cfg.expandSecrets(); err != nil {
		return nil, fmt.Errorf("config %s: %w", path, err)
	}
	if cfg.LoadBalancing == "" {
		cfg.LoadBalancing = "round_robin"
	}
	switch cfg.LoadBalancing {
	case "round_robin", "least_pending", "weighted":
	default:
		return nil, fmt.Errorf("config %s: unknown load_balancing %q (want round_robin, least_pending, or weighted)", path, cfg.LoadBalancing)
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
	if cfg.HealthCheckInterval <= 0 {
		return nil, fmt.Errorf("config %s: health_check_interval must be positive", path)
	}
	if !strings.HasPrefix(cfg.HealthCheckPath, "/") {
		return nil, fmt.Errorf("config %s: health_check_path must start with /", path)
	}
	if len(cfg.Backends) == 0 {
		return nil, fmt.Errorf("config %s: no backends configured", path)
	}
	urlByName := make(map[string]string, len(cfg.Backends))
	for i, b := range cfg.Backends {
		if prev, ok := urlByName[b.Name]; ok && prev != b.URL {
			return nil, fmt.Errorf("config %s: backend name %q is used for two different urls", path, b.Name)
		}
		urlByName[b.Name] = b.URL
		if b.Name == "" || b.URL == "" || len(b.Models) == 0 {
			return nil, fmt.Errorf("config %s: backends[%d] missing name, url, or models", path, i)
		}
		if err := checkHTTPURL(b.URL); err != nil {
			return nil, fmt.Errorf("config %s: backend %s url: %w", path, b.Name, err)
		}
		if b.PathPrefix != "" && !strings.HasPrefix(b.PathPrefix, "/") {
			return nil, fmt.Errorf("config %s: backend %s path_prefix must start with /", path, b.Name)
		}
		if b.Weight < 0 {
			return nil, fmt.Errorf("config %s: backend %s weight must not be negative", path, b.Name)
		}
	}

	for i, k := range cfg.APIKeys {
		if strings.TrimSpace(k) == "" {
			return nil, fmt.Errorf("config %s: api_keys[%d] is empty", path, i)
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
