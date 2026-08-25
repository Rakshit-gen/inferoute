// Package backend tracks a set of inference backends grouped by the models
// they serve, health-checks them, and hands out the next healthy one for a
// given model in round-robin order.
package backend

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"sync/atomic"
	"time"

	"github.com/Rakshit-gen/inferoute/internal/config"
)

type Backend struct {
	Name       string
	URL        *url.URL
	PathPrefix string
	APIKey     string

	healthy atomic.Bool
}

func (b *Backend) Healthy() bool { return b.healthy.Load() }

type Pool struct {
	all      []*Backend
	byModel  map[string][]*Backend
	counters map[string]*atomic.Uint64
}

func NewPool(specs []config.Backend) (*Pool, error) {
	p := &Pool{
		byModel:  make(map[string][]*Backend),
		counters: make(map[string]*atomic.Uint64),
	}
	seen := make(map[string]*Backend)
	for _, s := range specs {
		u, err := url.Parse(s.URL)
		if err != nil {
			return nil, fmt.Errorf("backend %s: invalid url %q: %w", s.Name, s.URL, err)
		}
		b, ok := seen[s.Name]
		if !ok {
			b = &Backend{Name: s.Name, URL: u, PathPrefix: s.PathPrefix, APIKey: s.APIKey}
			b.healthy.Store(true) // assume healthy until the first check
			seen[s.Name] = b
			p.all = append(p.all, b)
		}
		for _, m := range s.Models {
			p.byModel[m] = append(p.byModel[m], b)
			if p.counters[m] == nil {
				p.counters[m] = new(atomic.Uint64)
			}
		}
	}
	return p, nil
}

// Next returns the next healthy backend serving model, in round-robin order
// among the backends registered for that model. It returns an error if no
// backend serves the model or all of them are currently unhealthy.
func (p *Pool) Next(model string) (*Backend, error) {
	candidates := p.byModel[model]
	if len(candidates) == 0 {
		return nil, fmt.Errorf("no backend serves model %q", model)
	}
	start := p.counters[model].Add(1)
	for i := 0; i < len(candidates); i++ {
		b := candidates[(int(start)+i)%len(candidates)]
		if b.Healthy() {
			return b, nil
		}
	}
	return nil, fmt.Errorf("all backends for model %q are unhealthy", model)
}

func (p *Pool) MarkUnhealthy(b *Backend) { b.healthy.Store(false) }

// Status is a point-in-time snapshot of one backend, for introspection.
type Status struct {
	Name    string `json:"name"`
	URL     string `json:"url"`
	Healthy bool   `json:"healthy"`
}

// Snapshot returns the current health of every backend in the pool.
func (p *Pool) Snapshot() []Status {
	out := make([]Status, len(p.all))
	for i, b := range p.all {
		out[i] = Status{Name: b.Name, URL: b.URL.String(), Healthy: b.Healthy()}
	}
	return out
}

// StartHealthChecks polls every backend's health path on interval and
// updates its healthy flag until ctx is canceled.
func (p *Pool) StartHealthChecks(ctx context.Context, path string, interval time.Duration) {
	client := &http.Client{Timeout: 2 * time.Second}
	check := func(b *Backend) {
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
