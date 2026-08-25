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
