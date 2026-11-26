package main

import (
	"bytes"
	"container/list"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Rakshit-gen/inferoute/internal/backend"
	"github.com/Rakshit-gen/inferoute/internal/config"
	"github.com/Rakshit-gen/inferoute/internal/proxy"
)

// The affinity benchmark compares load-balancing strategies on multi-turn
// chat against mock backends that model a prefix (KV) cache the way vLLM and
// SGLang reuse one: prefill time is proportional to the prompt characters not
// already cached on that backend, and each backend runs a fixed number of
// requests at once, so a hotspot shows up as queueing. It measures routing
// locality, not a real inference engine.
const (
	affBackends     = 4
	affSlots        = 8                     // concurrent requests per backend
	affCacheEntries = 512                   // cached prefixes per backend (LRU)
	affDecode       = 20 * time.Millisecond // fixed per-request generation time
	affPrefillPerKB = 25 * time.Millisecond // prefill cost per 1000 uncached chars
	affTurns        = 6
)

var (
	affSystem = strings.Repeat("You are a support assistant for a cloud provider. Answer precisely. ", 30) // ~2 KB
	affUser   = strings.Repeat("Here is more detail about the problem I am seeing. ", 4)                   // ~200 B
	affReply  = strings.Repeat("Here is what to check next, step by step. ", 10)                           // ~400 B
)

type msg struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// prefixBackend is one mock engine with an LRU of cached message prefixes.
type prefixBackend struct {
	slots chan struct{}

	mu    sync.Mutex
	lru   *list.List // of uint64 prefix hashes, front = most recent
	index map[uint64]*list.Element

	promptChars, cachedChars, requests atomic.Int64
}

func newPrefixBackend() *prefixBackend {
	return &prefixBackend{slots: make(chan struct{}, affSlots), lru: list.New(), index: map[uint64]*list.Element{}}
}

// prefixHashes returns h[i] = hash of msgs[:i+1].
func prefixHashes(msgs []msg) []uint64 {
	out := make([]uint64, len(msgs))
	h := uint64(14695981039346656037)
	for i, m := range msgs {
		for _, s := range []string{m.Role, "\x00", m.Content, "\x01"} {
			for j := 0; j < len(s); j++ {
				h = (h ^ uint64(s[j])) * 1099511628211
			}
		}
		out[i] = h
	}
	return out
}

func (pb *prefixBackend) touch(h uint64) {
	if e, ok := pb.index[h]; ok {
		pb.lru.MoveToFront(e)
		return
	}
	pb.index[h] = pb.lru.PushFront(h)
	if pb.lru.Len() > affCacheEntries {
		old := pb.lru.Remove(pb.lru.Back()).(uint64)
		delete(pb.index, old)
	}
}

func (pb *prefixBackend) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusOK) // health check
		return
	}
	var req struct {
		Messages []msg `json:"messages"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	pb.slots <- struct{}{}
	defer func() { <-pb.slots }()

	hashes := prefixHashes(req.Messages)
	// The reply this engine generates is cached along with the prompt, as a
	// real engine keeps the decoded tokens' KV entries.
	withReply := prefixHashes(append(req.Messages, msg{"assistant", affReply}))

	pb.mu.Lock()
	cached := 0
	for i := len(hashes) - 1; i >= 0; i-- {
		if e, ok := pb.index[hashes[i]]; ok {
			pb.lru.MoveToFront(e)
			cached = i + 1
			break
		}
	}
	for _, h := range withReply {
		pb.touch(h)
	}
	pb.mu.Unlock()

	total, hit := 0, 0
	for i, m := range req.Messages {
		total += len(m.Content)
		if i < cached {
			hit += len(m.Content)
		}
	}
	pb.promptChars.Add(int64(total))
	pb.cachedChars.Add(int64(hit))
	pb.requests.Add(1)

	time.Sleep(affDecode + time.Duration(total-hit)*affPrefillPerKB/1000)
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprintf(w, `{"choices":[{"message":{"role":"assistant","content":%q}}]}`, affReply)
}

type affResult struct {
	strategy           string
	hitRate            float64
	mean, p50, p99     time.Duration
	busiestShare       float64
	requests, failures int64
}

// runAffinity sends convs conversations of affTurns turns each, workers at a
// time, through a gateway using strategy. hotOpener gives every conversation
// the same opening user message, the worst case for affinity.
func runAffinity(ctx context.Context, strategy string, convs, workers int, hotOpener bool) affResult {
	var specs []config.Backend
	var engines []*prefixBackend
	for i := range affBackends {
		pb := newPrefixBackend()
		ln := mustListen()
		srv := &http.Server{Handler: pb}
		go srv.Serve(ln)
		defer srv.Close()
		engines = append(engines, pb)
		specs = append(specs, config.Backend{Name: fmt.Sprintf("engine-%d", i), URL: "http://" + ln.Addr().String(), Models: []string{model}})
	}
	pool, err := backend.NewPool(specs, strategy)
	must("build pool", err)
	url := serve(proxyMux(proxy.New(pool)))
	client := newClient(workers)

	var next, failures atomic.Int64
	var mu sync.Mutex
	var lat []time.Duration
	var wg sync.WaitGroup
	for range workers {
		wg.Go(func() {
			var local []time.Duration
			for {
				c := next.Add(1)
				if c > int64(convs) || ctx.Err() != nil {
					break
				}
				opener := fmt.Sprintf("Conversation %d: my deployment keeps failing. %s", c, affUser)
				if hotOpener {
					opener = "My deployment keeps failing. " + affUser
				}
				msgs := []msg{{"system", affSystem}, {"user", opener}}
				for turn := range affTurns {
					body, _ := json.Marshal(map[string]any{"model": model, "messages": msgs})
					t0 := time.Now()
					resp, err := client.Post(url, "application/json", bytes.NewReader(body))
					if err != nil {
						failures.Add(1)
						break
					}
					io.Copy(io.Discard, resp.Body)
					resp.Body.Close()
					local = append(local, time.Since(t0))
					if resp.StatusCode != http.StatusOK {
						failures.Add(1)
					}
					msgs = append(msgs, msg{"assistant", affReply}, msg{"user", fmt.Sprintf("Conversation %d, turn %d. %s", c, turn+1, affUser)})
				}
			}
			mu.Lock()
			lat = append(lat, local...)
			mu.Unlock()
		})
	}
	wg.Wait()

	var prompt, cached, reqs, busiest int64
	for _, e := range engines {
		prompt += e.promptChars.Load()
		cached += e.cachedChars.Load()
		n := e.requests.Load()
		reqs += n
		busiest = max(busiest, n)
	}
	slices.Sort(lat)
	var sum time.Duration
	for _, l := range lat {
		sum += l
	}
	return affResult{
		strategy: strategy, hitRate: float64(cached) / float64(prompt),
		mean: sum / time.Duration(len(lat)), p50: pct(lat, 50), p99: pct(lat, 99),
		busiestShare: float64(busiest) / float64(reqs), requests: reqs, failures: failures.Load(),
	}
}

func affinityBench(ctx context.Context, convs, workers int) {
	fmt.Printf("\nprefix-affinity benchmark: %d backends x %d slots, %d conversations x %d turns, %d at a time\n",
		affBackends, affSlots, convs, affTurns, workers)
	fmt.Printf("mock engine: %s decode + %s per 1000 uncached prompt chars, LRU of %d prefixes\n",
		affDecode, affPrefillPerKB, affCacheEntries)
	for _, hot := range []bool{false, true} {
		if hot {
			fmt.Printf("\nworst case: every conversation opens with the same messages\n")
		} else {
			fmt.Printf("\ndistinct conversations\n")
		}
		fmt.Printf("%-16s %10s %10s %10s %10s %10s %9s\n", "strategy", "prefix hit", "mean", "p50", "p99", "busiest", "failures")
		for _, s := range []string{backend.RoundRobin, backend.LeastPending, backend.PrefixAffinity} {
			r := runAffinity(ctx, s, convs, workers, hot)
			fmt.Printf("%-16s %9.1f%% %10s %10s %10s %9.1f%% %9d\n", r.strategy, 100*r.hitRate, d(r.mean), d(r.p50), d(r.p99), 100*r.busiestShare, r.failures)
		}
	}
	fmt.Println()
}
