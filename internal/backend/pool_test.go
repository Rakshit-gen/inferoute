package backend

import (
	"testing"

	"github.com/Rakshit-gen/inferoute/internal/config"
)

func mustPool(t *testing.T, specs []config.Backend) *Pool {
	t.Helper()
	p, err := NewPool(specs)
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
		b, err := p.Next("llama3")
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
		b, err := p.Next("llama3")
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

	if _, err := p.Next("llama3"); err == nil {
		t.Fatal("expected error when all backends unhealthy, got nil")
	}
}

func TestNextErrorsForUnknownModel(t *testing.T) {
	p := mustPool(t, []config.Backend{
		{Name: "a", URL: "http://a", Models: []string{"llama3"}},
	})
	if _, err := p.Next("gpt-nonexistent"); err == nil {
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
