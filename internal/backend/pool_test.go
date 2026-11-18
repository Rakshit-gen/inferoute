package backend

import (
	"errors"
	"testing"
	"time"

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

	if _, _, err := p.Next("llama3"); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("err = %v, want ErrUnavailable", err)
	}
}

func TestNextErrorsForUnknownModel(t *testing.T) {
	p := mustPool(t, []config.Backend{
		{Name: "a", URL: "http://a", Models: []string{"llama3"}},
	})
	if _, _, err := p.Next("gpt-nonexistent"); !errors.Is(err, ErrUnknownModel) {
		t.Fatalf("err = %v, want ErrUnknownModel", err)
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

	// Hold the first request open; the next pick must go to the other one.
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

func TestRoundRobinSurvivesCounterOverflow(t *testing.T) {
	p := mustPool(t, []config.Backend{
		{Name: "a", URL: "http://a", Models: []string{"m"}},
		{Name: "b", URL: "http://b", Models: []string{"m"}},
	})
	p.counters["m"].Store(^uint64(0) - 1)
	for i := 0; i < 4; i++ {
		if _, _, err := p.Next("m"); err != nil {
			t.Fatal(err)
		}
	}
}

func TestLeastPendingSharesTies(t *testing.T) {
	p := mustPool(t, []config.Backend{
		{Name: "a", URL: "http://a", Models: []string{"m"}},
		{Name: "b", URL: "http://b", Models: []string{"m"}},
	}, LeastPending)
	seen := map[string]int{}
	for i := 0; i < 10; i++ {
		b, release, err := p.Next("m")
		if err != nil {
			t.Fatal(err)
		}
		seen[b.Name]++
		release()
	}
	if seen["a"] != 5 || seen["b"] != 5 {
		t.Fatalf("idle backends should share sequential traffic, got %v", seen)
	}
}

func TestRepeatedBackendIsNotDoubleCounted(t *testing.T) {
	p := mustPool(t, []config.Backend{
		{Name: "a", URL: "http://a", Models: []string{"m"}},
		{Name: "a", URL: "http://a", Models: []string{"m", "n"}},
	})
	if len(p.byModel["m"]) != 1 || len(p.byModel["n"]) != 1 {
		t.Fatalf("byModel = %v", p.byModel)
	}
}

func TestSnapshotHidesURLCredentials(t *testing.T) {
	p := mustPool(t, []config.Backend{{Name: "a", URL: "http://u:secret@a:1/x?token=t", Models: []string{"m"}}})
	if got := p.Snapshot()[0].URL; got != "http://a:1/x" {
		t.Fatalf("URL = %q", got)
	}
}

func TestNextHonoursExclude(t *testing.T) {
	p := mustPool(t, []config.Backend{
		{Name: "a", URL: "http://a", Models: []string{"m"}},
		{Name: "b", URL: "http://b", Models: []string{"m"}},
	})
	first, _, _ := p.Next("m")
	second, _, err := p.Next("m", first)
	if err != nil || second == first {
		t.Fatalf("second = %v, err = %v", second, err)
	}
	if _, _, err := p.Next("m", first, second); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("err = %v, want ErrUnavailable", err)
	}
}

func TestEjectionOutlastsPassingHealthCheck(t *testing.T) {
	p := mustPool(t, []config.Backend{{Name: "a", URL: "http://a", Models: []string{"m"}}})
	b := p.all[0]
	p.MarkUnhealthy(b)
	b.healthy.Store(true) // what a passing probe does
	if b.Healthy() {
		t.Fatal("an ejected backend must stay out until the cooldown ends")
	}
	b.ejectedUntil.Store(time.Now().Add(-time.Second).UnixNano())
	if !b.Healthy() {
		t.Fatal("the backend should return after the cooldown")
	}
}

func TestConsecutiveFailuresEject(t *testing.T) {
	p := mustPool(t, []config.Backend{{Name: "a", URL: "http://a", Models: []string{"m"}}})
	b := p.all[0]
	p.ReportFailure(b)
	p.ReportFailure(b)
	p.ReportSuccess(b)
	p.ReportFailure(b)
	p.ReportFailure(b)
	if !b.Healthy() {
		t.Fatal("failures separated by a success must not eject")
	}
	p.ReportFailure(b)
	if b.Healthy() {
		t.Fatal("three consecutive failures must eject")
	}
}

func TestMaxConcurrentSpillsToOtherBackend(t *testing.T) {
	p := mustPool(t, []config.Backend{
		{Name: "a", URL: "http://a", Models: []string{"m"}, MaxConcurrent: 1},
		{Name: "b", URL: "http://b", Models: []string{"m"}, MaxConcurrent: 1},
	})
	first, release, err := p.Next("m")
	if err != nil {
		t.Fatal(err)
	}
	second, _, err := p.Next("m")
	if err != nil || second == first {
		t.Fatalf("second = %v, err = %v", second, err)
	}
	if _, _, err := p.Next("m"); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("err = %v, want ErrUnavailable when all are full", err)
	}
	release()
	if _, _, err := p.Next("m"); err != nil {
		t.Fatalf("a released slot should be usable: %v", err)
	}
}
