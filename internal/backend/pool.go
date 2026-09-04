// Package backend tracks a set of inference backends grouped by the models
// they serve, health-checks them, and hands out the next healthy one for a
// given model in round-robin order.
package backend

import (
	"context"
	"fmt"
	"math/rand/v2"
	"net/http"
	"net/url"
	"slices"
	"sync/atomic"
	"time"

	"github.com/Rakshit-gen/inferoute/internal/config"
)

// Load-balancing strategies, as accepted in the "load_balancing" config key.
const (
	RoundRobin   = "round_robin"
	LeastPending = "least_pending"
	Weighted     = "weighted"
)

type Backend struct {
	Name       string
	URL        *url.URL
	PathPrefix string
	APIKey     string
	Models     []string
	Weight     int

	healthy  atomic.Bool
	inflight atomic.Int64 // requests currently being proxied (least_pending)
	checking atomic.Bool  // a health check for this backend is in flight
}

func (b *Backend) Healthy() bool { return b.healthy.Load() }

type Pool struct {
	all      []*Backend
	byModel  map[string][]*Backend
	counters map[string]*atomic.Uint64
	strategy string
}

// NewPool builds a pool from specs. strategy is one of RoundRobin (the
// default when empty or unrecognised), LeastPending, or Weighted.
func NewPool(specs []config.Backend, strategy ...string) (*Pool, error) {
	p := &Pool{
		byModel:  make(map[string][]*Backend),
		counters: make(map[string]*atomic.Uint64),
		strategy: RoundRobin,
	}
	if len(strategy) > 0 && (strategy[0] == LeastPending || strategy[0] == Weighted) {
		p.strategy = strategy[0]
	}
	seen := make(map[string]*Backend)
	for _, s := range specs {
		u, err := url.Parse(s.URL)
		if err != nil {
			return nil, fmt.Errorf("backend %s: invalid url %q: %w", s.Name, s.URL, err)
		}
		b, ok := seen[s.Name]
		if !ok {
			b = &Backend{Name: s.Name, URL: u, PathPrefix: s.PathPrefix, APIKey: s.APIKey, Weight: s.Weight}
			b.healthy.Store(true) // assume healthy until the first check
			seen[s.Name] = b
			p.all = append(p.all, b)
		}
		for _, m := range s.Models {
			p.byModel[m] = append(p.byModel[m], b)
			if p.counters[m] == nil {
				p.counters[m] = new(atomic.Uint64)
			}
			if !slices.Contains(b.Models, m) {
				b.Models = append(b.Models, m)
			}
		}
	}
	return p, nil
}

// Next returns a healthy backend serving model, chosen by the pool's
// load-balancing strategy, plus a release func the caller must call once it
// has finished proxying to that backend (it decrements the in-flight count
// the least_pending strategy reads; a no-op for the others). It returns an
// error if no backend serves the model or all of them are currently
// unhealthy.
func (p *Pool) Next(model string) (*Backend, func(), error) {
	candidates := p.byModel[model]
	if len(candidates) == 0 {
		return nil, nil, fmt.Errorf("no backend serves model %q", model)
	}

	var b *Backend
	switch p.strategy {
	case LeastPending:
		b = pickLeastPending(candidates)
	case Weighted:
		b = pickWeighted(candidates)
	default:
		b = p.pickRoundRobin(model, candidates)
	}
	if b == nil {
		return nil, nil, fmt.Errorf("all backends for model %q are unhealthy", model)
	}

	if p.strategy == LeastPending {
		b.inflight.Add(1)
		return b, func() { b.inflight.Add(-1) }, nil
	}
	return b, func() {}, nil
}

func (p *Pool) pickRoundRobin(model string, candidates []*Backend) *Backend {
	start := p.counters[model].Add(1)
	for i := 0; i < len(candidates); i++ {
		b := candidates[(int(start)+i)%len(candidates)]
		if b.Healthy() {
			return b
		}
	}
	return nil
}

func pickLeastPending(candidates []*Backend) *Backend {
	var best *Backend
	var bestN int64
	for _, b := range candidates {
		if !b.Healthy() {
			continue
		}
		if n := b.inflight.Load(); best == nil || n < bestN {
			best, bestN = b, n
		}
	}
	return best
}

func pickWeighted(candidates []*Backend) *Backend {
	total := 0
	for _, b := range candidates {
		if b.Healthy() {
			total += weightOf(b)
		}
	}
	if total == 0 {
		return nil
	}
	r := rand.IntN(total)
	for _, b := range candidates {
		if !b.Healthy() {
			continue
		}
		if r -= weightOf(b); r < 0 {
			return b
		}
	}
	return nil
}

func weightOf(b *Backend) int {
	if b.Weight <= 0 {
		return 1
	}
	return b.Weight
}

func (p *Pool) MarkUnhealthy(b *Backend) { b.healthy.Store(false) }

// Models returns every model name any backend in the pool serves, sorted
// and de-duplicated. Backs the OpenAI-compatible GET /v1/models.
func (p *Pool) Models() []string {
	out := make([]string, 0, len(p.byModel))
	for m := range p.byModel {
		out = append(out, m)
	}
	slices.Sort(out)
	return out
}

// Status is a point-in-time snapshot of one backend, for introspection.
type Status struct {
	Name    string   `json:"name"`
	URL     string   `json:"url"`
	Models  []string `json:"models"`
	Healthy bool     `json:"healthy"`
}

// Snapshot returns the current health of every backend in the pool.
func (p *Pool) Snapshot() []Status {
	out := make([]Status, len(p.all))
	for i, b := range p.all {
		out[i] = Status{Name: b.Name, URL: b.URL.String(), Models: b.Models, Healthy: b.Healthy()}
	}
	return out
}

// StartHealthChecks polls every backend's health path on interval and
// updates its healthy flag until ctx is canceled.
func (p *Pool) StartHealthChecks(ctx context.Context, path string, interval time.Duration) {
	client := &http.Client{Timeout: 2 * time.Second}
	check := func(b *Backend) {
		// Skip if the previous tick's check for this backend hasn't returned
		// yet, so a slow check can't land after (and overwrite) a newer one.
		if !b.checking.CompareAndSwap(false, true) {
			return
		}
		defer b.checking.Store(false)

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, b.URL.String()+path, nil)
		if err != nil {
			b.healthy.Store(false)
			return
		}
		resp, err := client.Do(req)
		if err != nil {
			b.healthy.Store(false)
			return
		}
		resp.Body.Close()
		b.healthy.Store(resp.StatusCode < 500)
	}

	for _, b := range p.all {
		check(b) // don't wait a full interval to know initial state
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			for _, b := range p.all {
				go check(b)
			}
		}
	}
}
