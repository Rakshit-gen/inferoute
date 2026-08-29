// Package proxy implements the OpenAI-compatible HTTP handler that reads
// the "model" field from the request body (resolving it through Aliases
// first, if set), picks a healthy backend for it, and forwards the request,
// retrying the next backend on failure and streaming the response through
// unbuffered for SSE. When a Cache is configured, chat requests — streaming
// included — are served from and populate a semantic cache instead of
// always hitting a backend.
package proxy

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/Rakshit-gen/inferoute/internal/backend"
	"github.com/Rakshit-gen/inferoute/internal/cache"
	"github.com/Rakshit-gen/inferoute/internal/metrics"
)

type Handler struct {
	Cache  *cache.Cache // nil disables semantic caching
	Client *http.Client

	pool    atomic.Pointer[backend.Pool]
	aliases atomic.Pointer[map[string]string] // requested model -> model backends actually serve
}

func New(pool *backend.Pool) *Handler {
	h := &Handler{
		Client: &http.Client{Timeout: 5 * time.Minute}, // generous: covers slow model generations
	}
	h.pool.Store(pool)
	empty := map[string]string{}
	h.aliases.Store(&empty)
	return h
}

// SetPool atomically swaps the backend pool a running Handler routes
// against, so it can pick up config changes (e.g. a SIGHUP reload) without
// disrupting in-flight requests.
func (h *Handler) SetPool(pool *backend.Pool) { h.pool.Store(pool) }

// CurrentPool returns the pool currently in use, for introspection.
func (h *Handler) CurrentPool() *backend.Pool { return h.pool.Load() }

// SetAliases atomically swaps the requested-model -> served-model map.
func (h *Handler) SetAliases(aliases map[string]string) { h.aliases.Store(&aliases) }

// Aliases returns the current requested-model -> served-model map, for
// introspection.
func (h *Handler) Aliases() map[string]string { return *h.aliases.Load() }

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "reading request body: "+err.Error(), http.StatusBadRequest)
		return
	}
	r.Body.Close()

	var parsed struct {
		Model    string          `json:"model"`
		Stream   bool            `json:"stream"`
		Messages []cache.Message `json:"messages"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil || parsed.Model == "" {
		http.Error(w, `request body must be JSON with a "model" field`, http.StatusBadRequest)
		return
	}
	model := parsed.Model
	if alias, ok := (*h.aliases.Load())[model]; ok {
		model = alias
		// The backend needs to see the resolved model name, not the alias
		// the client asked for — rewrite just that field, preserving
		// everything else in the body byte-for-byte.
		var raw map[string]json.RawMessage
		if err := json.Unmarshal(body, &raw); err == nil {
			if resolved, err := json.Marshal(model); err == nil {
				raw["model"] = resolved
				if rewritten, err := json.Marshal(raw); err == nil {
					body = rewritten
				}
			}
		}
	}
	defer metrics.ObserveDuration(model, start)

	cacheable := h.Cache != nil && len(parsed.Messages) > 0
	var cacheKey string
	if cacheable {
		cacheKey = cache.PromptText(parsed.Messages)
		if cached, ct, hit, err := h.Cache.Lookup(r.Context(), cacheKey); err != nil {
			log.Printf("inferoute: cache lookup: %v", err)
			metrics.CacheLookups.WithLabelValues("error").Inc()
		} else if hit {
			metrics.CacheLookups.WithLabelValues("hit").Inc()
			w.Header().Set("Content-Type", ct)
			w.Header().Set("X-Inferoute-Cache", "hit")
			w.Header().Set("X-Inferoute-Backend", "cache")
			w.Write(cached)
			metrics.RequestsTotal.WithLabelValues(model, "cache", "200").Inc()
			return
		} else {
			metrics.CacheLookups.WithLabelValues("miss").Inc()
		}
	}

	pool := h.pool.Load()
	const maxAttempts = 3
	var lastErr error
	for attempt := 0; attempt < maxAttempts; attempt++ {
		b, err := pool.Next(model)
		if err != nil {
			metrics.RequestsTotal.WithLabelValues(model, "none", "503").Inc()
			http.Error(w, err.Error(), http.StatusServiceUnavailable)
			return
		}

		ok, status, captured, contentType := h.forward(w, r, b, body, cacheable)
		if ok {
			metrics.RequestsTotal.WithLabelValues(model, b.Name, strconv.Itoa(status)).Inc()
			if cacheable && status == http.StatusOK && len(captured) > 0 {
				go h.storeInCache(cacheKey, captured, contentType)
			}
			return
		}
		lastErr = errors.New("backend " + b.Name + " failed")
		pool.MarkUnhealthy(b)
		log.Printf("inferoute: %s, retrying (attempt %d/%d)", lastErr, attempt+1, maxAttempts)
	}
	metrics.RequestsTotal.WithLabelValues(model, "none", "502").Inc()
	http.Error(w, "all backends failed: "+lastErr.Error(), http.StatusBadGateway)
}

func (h *Handler) storeInCache(key string, response []byte, contentType string) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := h.Cache.Store(ctx, key, response, contentType); err != nil {
		log.Printf("inferoute: cache store: %v", err)
	}
}

// forward sends the request to b and streams the response to w. It returns
// ok=false (without having written a response) if the backend could not be
// reached or returned a 5xx, so the caller can retry against another
// backend. Once any bytes of a backend's response have been written to w,
// failures are terminal — the caller already has an open HTTP response it
// can't restart. When tee is true, the response body (including an SSE
// stream, verbatim) is also returned in captured, along with the response's
// content type, for the caller to cache.
func (h *Handler) forward(w http.ResponseWriter, r *http.Request, b *backend.Backend, body []byte, tee bool) (ok bool, status int, captured []byte, contentType string) {
	outURL := *b.URL
	outURL.Path = b.PathPrefix + r.URL.Path
	outURL.RawQuery = r.URL.RawQuery

	req, err := http.NewRequestWithContext(r.Context(), r.Method, outURL.String(), bytes.NewReader(body))
	if err != nil {
		return false, 0, nil, ""
	}
	req.Header = r.Header.Clone()
	if b.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+b.APIKey)
	}

	resp, err := h.Client.Do(req)
	if err != nil {
		return false, 0, nil, ""
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 500 {
		return false, 0, nil, ""
	}

	ct := resp.Header.Get("Content-Type")
	var buf bytes.Buffer

	for k, vs := range resp.Header {
		for _, v := range vs {
			w.Header().Add(k, v)
		}
	}
	// Tell the caller which backend actually served this — the playground
	// and dashboard surface it, and it pairs with X-Inferoute-Cache.
	w.Header().Set("X-Inferoute-Backend", b.Name)
	w.WriteHeader(resp.StatusCode)

	flusher, canFlush := w.(http.Flusher)
	rbuf := make([]byte, 4096)
	for {
		n, readErr := resp.Body.Read(rbuf)
		if n > 0 {
			if _, err := w.Write(rbuf[:n]); err != nil {
				return true, resp.StatusCode, nil, "" // client disconnected; nothing more to do
			}
			if tee {
				buf.Write(rbuf[:n])
			}
			if canFlush {
				flusher.Flush() // keeps SSE/streaming chunks arriving as they're produced
			}
		}
		if readErr != nil {
			if tee {
				return true, resp.StatusCode, buf.Bytes(), ct
			}
			return true, resp.StatusCode, nil, "" // io.EOF or a mid-stream error: response already started, can't retry
		}
	}
}
