// Command loadtest drives the inferoute proxy end to end — API-key auth →
// rate limit → model resolution → backend selection → forward — against
// mock OpenAI-compatible backends, and reports throughput, the proxy's
// added latency over a direct backend call, and failover behaviour when a
// backend dies mid-load.
//
// Everything runs in-process with a real net/http server on a loopback
// port, so every hop on the hot path is real kernel TCP. Redis (for the
// shared rate limiter) is used if reachable at REDIS_ADDR, else that
// scenario is skipped.
//
// Usage:
//
//	go run ./cmd/loadtest -conns 96 -duration 20s
//
// Numbers land in README.md; re-run after any change to the request path.
package main

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Rakshit-gen/inferoute/internal/apiauth"
	"github.com/Rakshit-gen/inferoute/internal/backend"
	"github.com/Rakshit-gen/inferoute/internal/config"
	"github.com/Rakshit-gen/inferoute/internal/proxy"
	"github.com/Rakshit-gen/inferoute/internal/ratelimit"
	"github.com/redis/go-redis/v9"
)

const model = "llama3"

var reqBody = []byte(`{"model":"llama3","stream":false,"messages":[{"role":"user","content":"In one sentence, what is an API gateway?"}]}`)

// respBody is a ~320-byte OpenAI-compatible chat completion.
var respBody = []byte(`{"id":"chatcmpl-load","object":"chat.completion","created":0,"model":"llama3","choices":[{"index":0,"message":{"role":"assistant","content":"An API gateway is a single entry point that routes, authenticates, rate-limits and observes calls to a set of backend services."},"finish_reason":"stop"}],"usage":{"prompt_tokens":18,"completion_tokens":27,"total_tokens":45}}`)

func main() {
	conns := flag.Int("conns", 96, "concurrent connections (closed-loop workers)")
	duration := flag.Duration("duration", 20*time.Second, "measurement window per scenario")
	warmup := flag.Duration("warmup", 3*time.Second, "warmup window (discarded)")
	backendDelay := flag.Duration("backend-delay", 0, "artificial per-request latency at the mock backend")
	flag.Parse()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// --- mock OpenAI-compatible backend ---
	var beHits int64
	newBackend := func() (*http.Server, string) {
		mux := http.NewServeMux()
		mux.HandleFunc("/v1/chat/completions", func(w http.ResponseWriter, r *http.Request) {
			atomic.AddInt64(&beHits, 1)
			io.Copy(io.Discard, r.Body)
			if *backendDelay > 0 {
				time.Sleep(*backendDelay)
			}
			w.Header().Set("Content-Type", "application/json")
			w.Write(respBody)
		})
		mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }) // health
		ln := mustListen()
		srv := &http.Server{Handler: mux}
		go srv.Serve(ln)
		return srv, "http://" + ln.Addr().String()
	}
	be, beURL := newBackend()
	defer be.Close()

	// --- inferoute proxy, wired like cmd/inferouted ---
	pool, err := backend.NewPool([]config.Backend{{Name: "mock", URL: beURL, Models: []string{model}}}, backend.RoundRobin)
	must("build pool", err)
	go pool.StartHealthChecks(ctx, "/", time.Minute)

	h := proxy.New(pool)

	gwURL := serve(proxyMux(h)) // no auth, no rate limit
	authGate := apiauth.New([]string{"lt-secret-key"})
	gwAuthURL := serve(proxyMux(authGate.Middleware(h)))
	memRL := ratelimit.New(1e9, 1e9) // limit so high it never trips: measures middleware cost only
	gwMemRLURL := serve(proxyMux(ratelimit.Middleware(memRL, h)))

	client := newClient(*conns)
	authHdr := http.Header{"Authorization": {"Bearer lt-secret-key"}}

	warm(client, gwURL, nil)
	warm(client, beURL+"/v1/chat/completions", nil)
	time.Sleep(300 * time.Millisecond)

	fmt.Printf("\ninferoute proxy load test\n")
	fmt.Printf("conns=%d  duration=%s  backend-delay=%s  backend=in-process mock\n", *conns, *duration, *backendDelay)
	fmt.Printf("go %s  %s\n\n", goVersion(), time.Now().Format(time.RFC3339))

	results := []result{
		run("backend direct (baseline)", client, beURL+"/v1/chat/completions", nil, *conns, *warmup, *duration),
		run("proxy: passthrough", client, gwURL, nil, *conns, *warmup, *duration),
		run("proxy: + API-key auth", client, gwAuthURL, authHdr, *conns, *warmup, *duration),
		run("proxy: + rate limit (in-memory)", client, gwMemRLURL, authHdr, *conns, *warmup, *duration),
	}
	if rdb := dialRedis(); rdb != nil {
		rdb.Close()
		redisRL := ratelimit.NewRedis(redisAddr(), 1e9, 1e9)
		gwRedisRLURL := serve(proxyMux(ratelimit.Middleware(redisRL, h)))
		results = append(results, run("proxy: + rate limit (Redis, token bucket)", client, gwRedisRLURL, authHdr, *conns, *warmup, *duration))
	} else {
		fmt.Printf("(Redis not reachable at %s — skipping the shared rate-limit scenario)\n\n", redisAddr())
	}

	base := results[0]
	pass := results[1]
	fmt.Printf("\n%-42s %10s %10s %10s %10s %10s\n", "scenario", "req/s", "p50", "p90", "p99", "max")
	for _, r := range results {
		fmt.Printf("%-42s %10.0f %10s %10s %10s %10s\n", r.name, r.rps, d(r.p50), d(r.p90), d(r.p99), d(r.max))
	}
	fmt.Printf("\nadded latency (proxy passthrough - backend direct):\n")
	fmt.Printf("  p50 +%s   p90 +%s   p99 +%s\n", d(pass.p50-base.p50), d(pass.p90-base.p90), d(pass.p99-base.p99))
	if base.errs+pass.errs > 0 {
		fmt.Printf("  errors: baseline=%d passthrough=%d\n", base.errs, pass.errs)
	}

	// --- failover: two backends, kill one mid-load ---
	fmt.Printf("\nfailover (one of two backends killed mid-load, round-robin, retry budget 3):\n")
	beA, beAURL := newBackend()
	beB, beBURL := newBackend()
	defer beA.Close()
	defer beB.Close()
	foPool, err := backend.NewPool([]config.Backend{
		{Name: "a", URL: beAURL, Models: []string{model}},
		{Name: "b", URL: beBURL, Models: []string{model}},
	}, backend.RoundRobin)
	must("build failover pool", err)
	foCtx, foCancel := context.WithCancel(ctx)
	go foPool.StartHealthChecks(foCtx, "/", 2*time.Second)
	defer foCancel()
	foURL := serve(proxyMux(proxy.New(foPool)))

	warm(client, foURL, nil)
	var foTotal, foErr, foErrAfterKill int64
	var killed atomic.Bool
	var maxAfterKill atomic.Int64
	var tKillNanos, lastErrAfterKillNanos atomic.Int64
	foStop := time.Now().Add(12 * time.Second)
	var wg sync.WaitGroup
	for i := 0; i < *conns; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for time.Now().Before(foStop) {
				t0 := time.Now()
				_, code := postFull(client, foURL, nil)
				el := time.Since(t0)
				atomic.AddInt64(&foTotal, 1)
				bad := code != 200
				if bad {
					atomic.AddInt64(&foErr, 1)
				}
				if killed.Load() {
					if bad {
						atomic.AddInt64(&foErrAfterKill, 1)
						storeMax(&lastErrAfterKillNanos, time.Now().UnixNano())
					}
					storeMax(&maxAfterKill, int64(el))
				}
			}
		}()
	}
	time.Sleep(4 * time.Second)
	beB.Close() // hard-kill backend B
	tKillNanos.Store(time.Now().UnixNano())
	killed.Store(true)
	wg.Wait()

	// "settled" = time from the kill to the last request that still errored;
	// after that point every request succeeded (on the surviving backend).
	settled := "immediately (no request failed after the kill)"
	if last := lastErrAfterKillNanos.Load(); last > 0 {
		settled = d(time.Duration(last - tKillNanos.Load()))
	}
	fmt.Printf("  requests %d  errors total %d (%.2f%%)  errors after kill %d\n", foTotal, foErr,
		100*float64(foErr)/float64(foTotal), foErrAfterKill)
	fmt.Printf("  worst request latency after kill %s   failover settled %s\n", d(time.Duration(maxAfterKill.Load())), settled)

	fmt.Println()
}

type result struct {
	name               string
	rps                float64
	p50, p90, p99, max time.Duration
	errs               int64
}

func run(name string, client *http.Client, url string, hdr http.Header, conns int, warmup, dur time.Duration) result {
	deadline := time.Now().Add(warmup)
	var wg sync.WaitGroup
	for i := 0; i < conns; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for time.Now().Before(deadline) {
				postFull(client, url, hdr)
			}
		}()
	}
	wg.Wait()

	var total, errs int64
	var latMu sync.Mutex
	lat := make([]time.Duration, 0, 1<<20)
	stop := time.Now().Add(dur)
	start := time.Now()
	for i := 0; i < conns; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			local := make([]time.Duration, 0, 1<<16)
			for time.Now().Before(stop) {
				t0 := time.Now()
				body, code := postFull(client, url, hdr)
				el := time.Since(t0)
				if code != 200 || len(body) == 0 {
					atomic.AddInt64(&errs, 1)
				}
				atomic.AddInt64(&total, 1)
				local = append(local, el)
			}
			latMu.Lock()
			lat = append(lat, local...)
			latMu.Unlock()
		}()
	}
	wg.Wait()
	elapsed := time.Since(start)

	sort.Slice(lat, func(i, j int) bool { return lat[i] < lat[j] })
	return result{
		name: name, rps: float64(total) / elapsed.Seconds(),
		p50: pct(lat, 50), p90: pct(lat, 90), p99: pct(lat, 99), max: lat[len(lat)-1],
		errs: errs,
	}
}

func storeMax(a *atomic.Int64, v int64) {
	for {
		cur := a.Load()
		if v <= cur || a.CompareAndSwap(cur, v) {
			return
		}
	}
}

func pct(sorted []time.Duration, p int) time.Duration {
	if len(sorted) == 0 {
		return 0
	}
	i := p * len(sorted) / 100
	if i >= len(sorted) {
		i = len(sorted) - 1
	}
	return sorted[i]
}

func proxyMux(h http.Handler) http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/v1/", h)
	return mux
}

func serve(h http.Handler) string {
	ln := mustListen()
	go (&http.Server{Handler: h}).Serve(ln)
	return "http://" + ln.Addr().String() + "/v1/chat/completions"
}

func newClient(conns int) *http.Client {
	return &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			MaxIdleConns:        conns * 2,
			MaxIdleConnsPerHost: conns * 2,
			MaxConnsPerHost:     conns * 2,
			IdleConnTimeout:     90 * time.Second,
			DisableCompression:  true,
		},
	}
}

func postFull(c *http.Client, url string, hdr http.Header) ([]byte, int) {
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(reqBody))
	if err != nil {
		return nil, 0
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range hdr {
		req.Header[k] = v
	}
	resp, err := c.Do(req)
	if err != nil {
		return nil, 0
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return body, resp.StatusCode
}

func warm(c *http.Client, url string, hdr http.Header) {
	for i := 0; i < 5; i++ {
		postFull(c, url, hdr)
	}
}

func redisAddr() string {
	if v := os.Getenv("REDIS_ADDR"); v != "" {
		return v
	}
	return "localhost:6379"
}

func dialRedis() *redis.Client {
	rdb := redis.NewClient(&redis.Options{Addr: redisAddr()})
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := rdb.Ping(ctx).Err(); err != nil {
		rdb.Close()
		return nil
	}
	return rdb
}

func d(v time.Duration) string {
	switch {
	case v < 0:
		return "n/a"
	case v < time.Microsecond:
		return fmt.Sprintf("%dns", v.Nanoseconds())
	case v < time.Millisecond:
		return fmt.Sprintf("%.1fµs", float64(v.Nanoseconds())/1e3)
	default:
		return fmt.Sprintf("%.2fms", float64(v.Nanoseconds())/1e6)
	}
}

func mustListen() net.Listener {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	must("listen", err)
	return ln
}

func must(what string, err error) {
	if err != nil {
		fmt.Fprintf(os.Stderr, "loadtest: %s: %v\n", what, err)
		os.Exit(1)
	}
}

func goVersion() string {
	if v := os.Getenv("GOVERSION"); v != "" {
		return v
	}
	return "1.25"
}
