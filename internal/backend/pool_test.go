package backend

import (
	"testing"

	"github.com/Rakshit-gen/inferoute/internal/config"
)

func mustPool(t *testing.T, specs []config.Backend, strategy ...string) *Pool {
	t.Helper()
	p, err := NewPool(specs, strategy...)
	if err != nil {
		t.Fatalf("NewPool: %v", err)
	}
	return p
}

func TestNextRoundRobins(t *testing.T) {
	p := mustPool(t, []config.Backend{
		{Name: "a", URL: "http://a", Models: []string{"llama3"}},
		{Name: "b", URL: "http://b", Models: []string{"llama3"}},
	})

	seen := map[string]int{}
	for i := 0; i < 4; i++ {
		b, _, err := p.Next("llama3")
		if err != nil {
			t.Fatalf("Next: %v", err)
		}
		seen[b.Name]++
	}
	if seen["a"] != 2 || seen["b"] != 2 {
		t.Fatalf("expected even round-robin split, got %v", seen)
	}
}

func TestNextSkipsUnhealthy(t *testing.T) {
	p := mustPool(t, []config.Backend{
		{Name: "a", URL: "http://a", Models: []string{"llama3"}},
		{Name: "b", URL: "http://b", Models: []string{"llama3"}},
	})
	p.all[0].healthy.Store(false)

	for i := 0; i < 4; i++ {
		b, _, err := p.Next("llama3")
		if err != nil {
			t.Fatalf("Next: %v", err)
		}
		if b.Name != "b" {
			t.Fatalf("expected only healthy backend b, got %s", b.Name)
		}
	}
}

func TestNextErrorsWhenAllUnhealthy(t *testing.T) {
	p := mustPool(t, []config.Backend{
		{Name: "a", URL: "http://a", Models: []string{"llama3"}},
	})
	p.all[0].healthy.Store(false)

	if _, _, err := p.Next("llama3"); err == nil {
		t.Fatal("expected error when all backends unhealthy, got nil")
	}
}

func TestNextErrorsForUnknownModel(t *testing.T) {
	p := mustPool(t, []config.Backend{
		{Name: "a", URL: "http://a", Models: []string{"llama3"}},
	})
	if _, _, err := p.Next("gpt-nonexistent"); err == nil {
		t.Fatal("expected error for unregistered model, got nil")
	}
}

func TestSnapshotReportsHealth(t *testing.T) {
	p := mustPool(t, []config.Backend{
		{Name: "a", URL: "http://a", Models: []string{"llama3"}},
		{Name: "b", URL: "http://b", Models: []string{"llama3"}},
	})
	p.all[0].healthy.Store(false)

	snap := p.Snapshot()
	if len(snap) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(snap))
	}
	got := map[string]bool{}
	for _, s := range snap {
		got[s.Name] = s.Healthy
	}
	if got["a"] != false || got["b"] != true {
		t.Fatalf("unexpected snapshot: %+v", snap)
	}
}

func TestLeastPendingPrefersIdleBackend(t *testing.T) {
	p := mustPool(t, []config.Backend{
		{Name: "busy", URL: "http://busy", Models: []string{"llama3"}},
		{Name: "idle", URL: "http://idle", Models: []string{"llama3"}},
	}, LeastPending)

	// byModel keeps spec order, so the first pick (all idle) is "busy".
	// Hold that request open; the next pick must then go to "idle".
	b1, release1, err := p.Next("llama3")
	if err != nil {
		t.Fatalf("Next: %v", err)
	}
	b2, release2, err := p.Next("llama3")
	if err != nil {
		t.Fatalf("Next: %v", err)
	}
	if b2.Name == b1.Name {
		t.Fatalf("least_pending sent a second request to the already-busy %s", b1.Name)
	}
	release1()
	release2()

	// Once released, in-flight is back to zero everywhere.
	for _, b := range p.all {
		if n := b.inflight.Load(); n != 0 {
			t.Fatalf("%s left with in-flight=%d after release", b.Name, n)
		}
	}
}

func TestWeightedFavoursHeavierBackend(t *testing.T) {
	p := mustPool(t, []config.Backend{
		{Name: "light", URL: "http://light", Models: []string{"llama3"}, Weight: 1},
		{Name: "heavy", URL: "http://heavy", Models: []string{"llama3"}, Weight: 9},
	}, Weighted)

	seen := map[string]int{}
	for i := 0; i < 2000; i++ {
		b, _, err := p.Next("llama3")
		if err != nil {
			t.Fatalf("Next: %v", err)
		}
		seen[b.Name]++
	}
	if seen["heavy"] <= seen["light"]*3 {
		t.Fatalf("weight 9:1 should skew hard to heavy, got %v", seen)
	}
}

func TestModelsSortedUnique(t *testing.T) {
	p := mustPool(t, []config.Backend{
		{Name: "a", URL: "http://a", Models: []string{"mistral", "llama3"}},
		{Name: "b", URL: "http://b", Models: []string{"llama3"}},
	})
	got := p.Models()
	want := []string{"llama3", "mistral"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("Models() = %v, want %v", got, want)
	}
}
