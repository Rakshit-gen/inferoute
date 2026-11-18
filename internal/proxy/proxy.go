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
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Rakshit-gen/inferoute/internal/apiauth"
	"github.com/Rakshit-gen/inferoute/internal/apierr"
	"github.com/Rakshit-gen/inferoute/internal/backend"
	"github.com/Rakshit-gen/inferoute/internal/cache"
	"github.com/Rakshit-gen/inferoute/internal/metrics"
)

const (
	// DefaultMaxBodyBytes fits large prompts and inline images.
	DefaultMaxBodyBytes = 16 << 20
	bodyReadTimeout     = 30 * time.Second
	maxCacheWrites      = 16
)

type Handler struct {
	Cache  *cache.Cache // nil disables semantic caching
	Client *http.Client
	// MaxBodyBytes limits the size of a request body.
	MaxBodyBytes int64
	// UpstreamTimeout limits the wait for a backend's response headers.
	UpstreamTimeout time.Duration
	// StreamIdleTimeout limits the gap between response chunks.
	StreamIdleTimeout time.Duration

	slots    chan struct{} // nil means no concurrency limit
	stores   chan struct{} // limits concurrent background cache writes
	storesWG sync.WaitGroup

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
	transport.DialContext = (&net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}).DialContext
	transport.TLSHandshakeTimeout = 5 * time.Second

	h := &Handler{
		MaxBodyBytes:      DefaultMaxBodyBytes,
		UpstreamTimeout:   5 * time.Minute, // non-streaming generations send headers last
		StreamIdleTimeout: 2 * time.Minute,
		Client:            &http.Client{Transport: transport},
		stores:            make(chan struct{}, maxCacheWrites),
	}
	h.pool.Store(pool)
	empty := map[string]string{}
	h.aliases.Store(&empty)
	return h
}

// SetMaxConcurrent limits requests handled at once; n <= 0 removes the limit.
// Call it before serving.
func (h *Handler) SetMaxConcurrent(n int) {
	h.slots = nil
	if n > 0 {
		h.slots = make(chan struct{}, n)
	}
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
	if h.slots != nil {
		select {
		case h.slots <- struct{}{}:
			defer func() { <-h.slots }()
		default:
			metrics.Rejected.WithLabelValues("overloaded").Inc()
			w.Header().Set("Retry-After", "1")
			apierr.Write(w, http.StatusServiceUnavailable, apierr.Server, "overloaded", "the gateway is at capacity")
			return
		}
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
	if h.Cache != nil && !bypassesCache(r) {
		messages, cacheable = cache.ParseMessages(parsed.Messages)
	}
	var cacheKey, scope string
	var vector []float32
	if cacheable {
		cacheKey = cache.PromptText(messages)
		scope = cacheScope(r, body)
		if found, err := h.Cache.Lookup(r.Context(), scope, cacheKey); errors.Is(err, cache.ErrUnavailable) {
			cacheable = false // don't queue writes against a cache that is down
			metrics.CacheLookups.WithLabelValues("skipped").Inc()
		} else if err != nil {
			log.Printf("inferoute: cache lookup: %v", err)
			metrics.CacheLookups.WithLabelValues("error").Inc()
		} else if vector = found.Vector; found.Hit {
			metrics.CacheLookups.WithLabelValues("hit").Inc()
			w.Header().Set("Content-Type", found.ContentType)
			w.Header().Set("X-Inferoute-Cache", "hit")
			w.Header().Set("X-Inferoute-Backend", "cache")
			w.Write(found.Body)
			metrics.RequestsTotal.WithLabelValues(model, "cache", "200").Inc()
			return
		} else {
			metrics.CacheLookups.WithLabelValues("miss").Inc()
		}
	}

	const maxAttempts = 3
	var lastErr error
	var tried []*backend.Backend
	var failure *upstreamFailure
	for attempt := 0; attempt < maxAttempts; attempt++ {
		b, release, err := pool.Next(model, tried...)
		if err != nil {
			if attempt > 0 {
				break // every available backend has been tried
			}
			metrics.RequestsTotal.WithLabelValues(model, "none", "503").Inc()
			w.Header().Set("Retry-After", "1")
			apierr.Write(w, http.StatusServiceUnavailable, apierr.Server, "no_backend_available", err.Error())
			return
		}

		tried = append(tried, b)
		res := h.forward(w, r, b, body, cacheable)
		release()
		if res.wrote {
			metrics.RequestsTotal.WithLabelValues(model, b.Name, strconv.Itoa(res.status)).Inc()
			if res.broken {
				pool.ReportFailure(b)
				log.Printf("inferoute: backend %s failed mid-response", b.Name)
				if r.Context().Value(http.ServerContextKey) != nil {
					panic(http.ErrAbortHandler) // makes the truncation visible to the client
				}
				return
			}
			pool.ReportSuccess(b)
			if cacheable && res.status == http.StatusOK && len(res.captured) > 0 {
				select {
				case h.stores <- struct{}{}:
					h.storesWG.Go(func() {
						defer func() { <-h.stores }()
						h.storeInCache(scope, cacheKey, vector, res.captured, res.contentType)
					})
				default: // the cache is behind; skipping a write only costs a future miss
					metrics.CacheWrites.WithLabelValues("dropped").Inc()
				}
			}
			return
		}
		if r.Context().Err() != nil {
			// The caller went away; that says nothing about the backend.
			metrics.RequestsTotal.WithLabelValues(model, b.Name, "499").Inc()
			return
		}
		metrics.Failovers.WithLabelValues(b.Name).Inc()
		lastErr = errors.New("backend " + b.Name + " failed")
		if res.failure != nil {
			failure = res.failure
		}
		switch {
		case res.unreachable:
			pool.MarkUnhealthy(b)
		case res.failure.status != http.StatusTooManyRequests: // 429 means busy, not broken
			pool.ReportFailure(b)
		}
		log.Printf("inferoute: %s, retrying (attempt %d/%d)", lastErr, attempt+1, maxAttempts)
	}
	if failure != nil {
		metrics.RequestsTotal.WithLabelValues(model, failure.backend, strconv.Itoa(failure.status)).Inc()
		copyResponseHeaders(w.Header(), failure.header)
		w.Header().Del("Content-Length") // the kept body may be truncated
		w.Header().Set("X-Inferoute-Backend", failure.backend)
		w.WriteHeader(failure.status)
		_, _ = w.Write(failure.body)
		return
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
	out.Del("Cookie")
	out.Del("Expect")
	out.Set("Content-Type", "application/json") // the body was parsed as JSON
	return out
}

// copyResponseHeaders adds a backend's end-to-end headers to dst.
func copyResponseHeaders(dst, src http.Header) {
	src = src.Clone()
	stripHop(src)
	src.Del("Set-Cookie")
	for k, vs := range src {
		if strings.HasPrefix(k, "Access-Control-") || strings.HasPrefix(k, "X-Inferoute-") {
			continue
		}
		dst[k] = append(dst[k], vs...)
	}
}

// Drain waits for background cache writes to finish or ctx to end.
func (h *Handler) Drain(ctx context.Context) {
	done := make(chan struct{})
	go func() { h.storesWG.Wait(); close(done) }()
	select {
	case <-done:
	case <-ctx.Done():
	}
}

func bypassesCache(r *http.Request) bool {
	cc := strings.ToLower(r.Header.Get("Cache-Control"))
	return strings.Contains(cc, "no-store") || strings.Contains(cc, "no-cache")
}

// cacheScope covers everything that changes the answer other than the
// messages: the endpoint and all other body fields, including the resolved
// model and stream flag.
func cacheScope(r *http.Request, body []byte) string {
	var fields map[string]json.RawMessage
	_ = json.Unmarshal(body, &fields)
	delete(fields, "messages")
	canonical, _ := json.Marshal(fields) // map keys are sorted
	caller := apiauth.Identity(r.Context())
	if auth := r.Header.Get("Authorization"); caller == "" && auth != "" {
		caller = apiauth.Fingerprint(auth)
	}
	return cache.Scope(r.URL.Path, caller, string(canonical))
}

func (h *Handler) storeInCache(scope, key string, vector []float32, response []byte, contentType string) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := h.Cache.Store(ctx, scope, key, vector, response, contentType); err != nil {
		log.Printf("inferoute: cache store: %v", err)
		metrics.CacheWrites.WithLabelValues("error").Inc()
		return
	}
	metrics.CacheWrites.WithLabelValues("stored").Inc()
}

// outcome reports what one attempt against a backend did.
type outcome struct {
	wrote       bool // a response was started, so no retry is possible
	unreachable bool // no response was obtained from the backend
	status      int
	captured    []byte
	contentType string
	broken      bool             // the backend failed after the response started
	failure     *upstreamFailure // set when the backend answered with an error
}

// upstreamFailure is a retryable error response, kept to relay if no other
// backend succeeds.
type upstreamFailure struct {
	status  int
	header  http.Header
	body    []byte
	backend string
}

// maxCachedResponse is the largest response stored in the cache.
const maxCachedResponse = 1 << 20

// maxFailureBody bounds how much of an error response is kept.
const maxFailureBody = 64 << 10

// retryable reports whether another backend might answer differently.
func retryable(status int) bool {
	switch status {
	case http.StatusNotImplemented, http.StatusHTTPVersionNotSupported:
		return false
	}
	return status >= 500 || status == http.StatusTooManyRequests
}

// forward sends the request to b and streams the response to w. Unless wrote
// is set in the result, nothing has reached the client and the caller can
// retry another backend. When tee is true, the response body (including an
// SSE stream, verbatim) is also captured for the cache.
func (h *Handler) forward(w http.ResponseWriter, r *http.Request, b *backend.Backend, body []byte, tee bool) outcome {
	outURL := b.URL.JoinPath(b.PathPrefix, r.URL.EscapedPath())
	outURL.RawQuery = r.URL.RawQuery

	// One watchdog cancels the attempt: first if headers take too long, then
	// whenever the stream goes quiet.
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	watchdog := time.AfterFunc(h.UpstreamTimeout, cancel)
	defer watchdog.Stop()

	req, err := http.NewRequestWithContext(ctx, r.Method, outURL.String(), bytes.NewReader(body))
	if err != nil {
		return outcome{unreachable: true}
	}
	req.Header = upstreamHeaders(r.Header)
	if b.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+b.APIKey)
	}
	if tee {
		req.Header.Set("Accept-Encoding", "identity") // cached bytes must be replayable to anyone
	}

	resp, err := h.Client.Do(req)
	if err != nil {
		return outcome{unreachable: true}
	}
	defer resp.Body.Close()

	if retryable(resp.StatusCode) {
		errBody, _ := io.ReadAll(io.LimitReader(resp.Body, maxFailureBody))
		return outcome{failure: &upstreamFailure{resp.StatusCode, resp.Header, errBody, b.Name}}
	}

	ct := resp.Header.Get("Content-Type")
	tee = tee && resp.Header.Get("Content-Encoding") == ""
	var buf bytes.Buffer

	copyResponseHeaders(w.Header(), resp.Header)
	// Tell the caller which backend actually served this — the playground
	// and dashboard surface it, and it pairs with X-Inferoute-Cache.
	w.Header().Set("X-Inferoute-Backend", b.Name)
	w.WriteHeader(resp.StatusCode)
	res := outcome{wrote: true, status: resp.StatusCode}

	flusher, canFlush := w.(http.Flusher)
	rbuf := make([]byte, 4096)
	for {
		watchdog.Reset(h.StreamIdleTimeout)
		n, readErr := resp.Body.Read(rbuf)
		if n > 0 {
			if _, err := w.Write(rbuf[:n]); err != nil {
				return res // client disconnected; nothing more to do
			}
			if tee {
				if buf.Len()+n > maxCachedResponse {
					tee = false // too big to cache; stop holding it
					buf = bytes.Buffer{}
				} else {
					buf.Write(rbuf[:n])
				}
			}
			if canFlush {
				flusher.Flush() // keeps SSE/streaming chunks arriving as they're produced
			}
		}
		if readErr != nil {
			res.broken = readErr != io.EOF && r.Context().Err() == nil
			if tee && readErr == io.EOF {
				res.captured, res.contentType = buf.Bytes(), ct
			}
			return res
		}
	}
}
