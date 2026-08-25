// Package proxy implements the OpenAI-compatible HTTP handler that reads
// the "model" field from the request body, picks a healthy backend for it,
// and forwards the request, retrying the next backend on failure and
// streaming the response through unbuffered for SSE. When a Cache is
// configured, non-streaming chat requests are served from and populate a
// semantic cache instead of always hitting a backend.
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
	"strings"
	"time"

	"github.com/Rakshit-gen/inferoute/internal/backend"
	"github.com/Rakshit-gen/inferoute/internal/cache"
	"github.com/Rakshit-gen/inferoute/internal/metrics"
)

type Handler struct {
	Pool   *backend.Pool
	Cache  *cache.Cache // nil disables semantic caching
	Client *http.Client
}

func New(pool *backend.Pool) *Handler {
	return &Handler{
		Pool:   pool,
		Client: &http.Client{Timeout: 5 * time.Minute}, // generous: covers slow model generations
	}
}

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
	defer metrics.ObserveDuration(parsed.Model, start)

	cacheable := h.Cache != nil && !parsed.Stream && len(parsed.Messages) > 0
	var cacheKey string
	if cacheable {
		cacheKey = cache.PromptText(parsed.Messages)
		if cached, hit, err := h.Cache.Lookup(r.Context(), cacheKey); err != nil {
			log.Printf("inferoute: cache lookup: %v", err)
			metrics.CacheLookups.WithLabelValues("error").Inc()
		} else if hit {
			metrics.CacheLookups.WithLabelValues("hit").Inc()
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("X-Inferoute-Cache", "hit")
			w.Write(cached)
			metrics.RequestsTotal.WithLabelValues(parsed.Model, "cache", "200").Inc()
			return
		} else {
			metrics.CacheLookups.WithLabelValues("miss").Inc()
		}
	}

	const maxAttempts = 3
	var lastErr error
	for attempt := 0; attempt < maxAttempts; attempt++ {
		b, err := h.Pool.Next(parsed.Model)
		if err != nil {
			metrics.RequestsTotal.WithLabelValues(parsed.Model, "none", "503").Inc()
			http.Error(w, err.Error(), http.StatusServiceUnavailable)
			return
		}

		ok, status, captured := h.forward(w, r, b, body, cacheable)
		if ok {
			metrics.RequestsTotal.WithLabelValues(parsed.Model, b.Name, strconv.Itoa(status)).Inc()
			if cacheable && status == http.StatusOK && len(captured) > 0 {
				go h.storeInCache(cacheKey, captured)
			}
			return
		}
		lastErr = errors.New("backend " + b.Name + " failed")
		h.Pool.MarkUnhealthy(b)
		log.Printf("inferoute: %s, retrying (attempt %d/%d)", lastErr, attempt+1, maxAttempts)
	}
	metrics.RequestsTotal.WithLabelValues(parsed.Model, "none", "502").Inc()
	http.Error(w, "all backends failed: "+lastErr.Error(), http.StatusBadGateway)
}

func (h *Handler) storeInCache(key string, response []byte) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := h.Cache.Store(ctx, key, response); err != nil {
		log.Printf("inferoute: cache store: %v", err)
	}
}

// forward sends the request to b and streams the response to w. It returns
// ok=false (without having written a response) if the backend could not be
// reached or returned a 5xx, so the caller can retry against another
// backend. Once any bytes of a backend's response have been written to w,
// failures are terminal — the caller already has an open HTTP response it
// can't restart. When tee is true and the response isn't a stream, the
// response body is also returned in captured for the caller to cache.
func (h *Handler) forward(w http.ResponseWriter, r *http.Request, b *backend.Backend, body []byte, tee bool) (ok bool, status int, captured []byte) {
	outURL := *b.URL
	outURL.Path = r.URL.Path
	outURL.RawQuery = r.URL.RawQuery

	req, err := http.NewRequestWithContext(r.Context(), r.Method, outURL.String(), bytes.NewReader(body))
	if err != nil {
		return false, 0, nil
	}
	req.Header = r.Header.Clone()

	resp, err := h.Client.Do(req)
	if err != nil {
		return false, 0, nil
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 500 {
		return false, 0, nil
	}

	isSSE := strings.Contains(resp.Header.Get("Content-Type"), "text/event-stream")
	doTee := tee && !isSSE
	var buf bytes.Buffer

	for k, vs := range resp.Header {
		for _, v := range vs {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(resp.StatusCode)

	flusher, canFlush := w.(http.Flusher)
	rbuf := make([]byte, 4096)
	for {
		n, readErr := resp.Body.Read(rbuf)
		if n > 0 {
			if _, err := w.Write(rbuf[:n]); err != nil {
				return true, resp.StatusCode, nil // client disconnected; nothing more to do
			}
			if doTee {
				buf.Write(rbuf[:n])
			}
			if canFlush {
				flusher.Flush() // keeps SSE/streaming chunks arriving as they're produced
			}
		}
		if readErr != nil {
			if doTee {
				return true, resp.StatusCode, buf.Bytes()
			}
			return true, resp.StatusCode, nil // io.EOF or a mid-stream error: response already started, can't retry
		}
	}
}
