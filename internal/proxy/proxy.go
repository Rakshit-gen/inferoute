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
	"strings"
	"sync/atomic"
	"time"

	"github.com/Rakshit-gen/inferoute/internal/apierr"
	"github.com/Rakshit-gen/inferoute/internal/backend"
	"github.com/Rakshit-gen/inferoute/internal/cache"
	"github.com/Rakshit-gen/inferoute/internal/metrics"
)

const (
	// DefaultMaxBodyBytes fits large prompts and inline images.
	DefaultMaxBodyBytes = 16 << 20
	bodyReadTimeout     = 30 * time.Second
)

type Handler struct {
	Cache  *cache.Cache // nil disables semantic caching
	Client *http.Client
	// MaxBodyBytes limits the size of a request body.
	MaxBodyBytes int64

	pool    atomic.Pointer[backend.Pool]
	aliases atomic.Pointer[map[string]string] // requested model -> model backends actually serve
}

func New(pool *backend.Pool) *Handler {
	// http.DefaultTransport keeps only 2 idle connections per host. A
	// gateway funnels every request for a model at a handful of backends,
	// so under concurrent load that cap turns almost every request into a
	// fresh TCP (and TLS) handshake — connection churn, latency, and
	// ephemeral-port pressure. Raise the idle pool to match how this is
	// actually used; keep the transport's own timeouts modest so a dead
	// backend fails fast enough to retry another.
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.MaxIdleConns = 256
	transport.MaxIdleConnsPerHost = 64
	transport.IdleConnTimeout = 90 * time.Second

	h := &Handler{
		MaxBodyBytes: DefaultMaxBodyBytes,
		Client: &http.Client{
			Timeout:   5 * time.Minute, // generous: covers slow model generations
			Transport: transport,
		},
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
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		apierr.Write(w, http.StatusMethodNotAllowed, apierr.InvalidRequest, "method_not_allowed", "this endpoint accepts POST")
		return
	}
	// A deadline on the body alone: http.Server.ReadTimeout would also cancel
	// the request while a slow generation is running.
	rc := http.NewResponseController(w)
	_ = rc.SetReadDeadline(time.Now().Add(bodyReadTimeout))
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, h.MaxBodyBytes))
	_ = rc.SetReadDeadline(time.Time{})
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			apierr.Write(w, http.StatusRequestEntityTooLarge, apierr.InvalidRequest, "request_too_large", "request body is too large")
			return
		}
		apierr.Write(w, http.StatusBadRequest, apierr.InvalidRequest, "invalid_body", "could not read the request body")
		return
	}
	r.Body.Close()

	var parsed struct {
		Model    string          `json:"model"`
		Messages json.RawMessage `json:"messages"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil || parsed.Model == "" {
		apierr.Write(w, http.StatusBadRequest, apierr.InvalidRequest, "invalid_body", `request body must be JSON with a "model" field`)
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
	pool := h.pool.Load()
	if !pool.Serves(model) {
		metrics.RequestsTotal.WithLabelValues("unknown", "none", "404").Inc()
		apierr.Write(w, http.StatusNotFound, apierr.InvalidRequest, "model_not_found", "no backend serves this model")
		return
	}
	defer metrics.ObserveDuration(model, start)

	var messages []cache.Message
	cacheable := false
	if h.Cache != nil {
		messages, cacheable = cache.ParseMessages(parsed.Messages)
	}
	var cacheKey string
	if cacheable {
		cacheKey = cache.PromptText(messages)
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

	const maxAttempts = 3
	var lastErr error
	for attempt := 0; attempt < maxAttempts; attempt++ {
		b, release, err := pool.Next(model)
		if err != nil {
			metrics.RequestsTotal.WithLabelValues(model, "none", "503").Inc()
			w.Header().Set("Retry-After", "1")
			apierr.Write(w, http.StatusServiceUnavailable, apierr.Server, "no_backend_available", err.Error())
			return
		}

		ok, status, captured, contentType := h.forward(w, r, b, body, cacheable)
		release()
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
	apierr.Write(w, http.StatusBadGateway, apierr.Server, "all_backends_failed", "all backends failed: "+lastErr.Error())
}

// hopHeaders apply to a single connection and are never forwarded.
var hopHeaders = []string{
	"Connection", "Proxy-Connection", "Keep-Alive", "Proxy-Authenticate",
	"Proxy-Authorization", "Te", "Trailer", "Transfer-Encoding", "Upgrade",
}

// stripHop removes hop-by-hop headers, including any Connection nominates.
func stripHop(h http.Header) {
	for _, v := range h.Values("Connection") {
		for _, name := range strings.Split(v, ",") {
			h.Del(strings.TrimSpace(name))
		}
	}
	for _, name := range hopHeaders {
		h.Del(name)
	}
}

// upstreamHeaders returns the client's headers as sent to a backend.
func upstreamHeaders(in http.Header) http.Header {
	out := in.Clone()
	stripHop(out)
	return out
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
	req.Header = upstreamHeaders(r.Header)
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
