package backend

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
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

func probeServer(t *testing.T, h http.HandlerFunc) string {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return srv.URL
}

// runProbes starts health checks and waits for the first one to land.
func runProbes(t *testing.T, p *Pool, path string) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { p.StartHealthChecks(ctx, path, time.Hour); close(done) }()
	time.Sleep(100 * time.Millisecond)
	cancel()
	<-done
}

func TestHealthCheckJoinsBasePath(t *testing.T) {
	var got string
	url := probeServer(t, func(w http.ResponseWriter, r *http.Request) { got = r.URL.Path })
	p := mustPool(t, []config.Backend{{Name: "a", URL: url + "/base/", Models: []string{"m"}}})
	runProbes(t, p, "/healthz")
	if got != "/base/healthz" {
		t.Fatalf("probe path = %q", got)
	}
}

func TestHealthCheckSendsAPIKey(t *testing.T) {
	var got string
	url := probeServer(t, func(w http.ResponseWriter, r *http.Request) { got = r.Header.Get("Authorization") })
	p := mustPool(t, []config.Backend{{Name: "a", URL: url, Models: []string{"m"}, APIKey: "k"}})
	runProbes(t, p, "/")
	if got != "Bearer k" {
		t.Fatalf("Authorization = %q", got)
	}
}

func TestCancelledProbeLeavesHealthAlone(t *testing.T) {
	url := probeServer(t, func(w http.ResponseWriter, r *http.Request) { time.Sleep(300 * time.Millisecond) })
	p := mustPool(t, []config.Backend{{Name: "a", URL: url, Models: []string{"m"}}})
	runProbes(t, p, "/")
	if !p.all[0].healthy.Load() {
		t.Fatal("a cancelled probe must not mark the backend down")
	}
}

func TestProbeThreshold(t *testing.T) {
	p := mustPool(t, []config.Backend{{Name: "a", URL: "http://a", Models: []string{"m"}}})
	b := p.all[0]
	b.recordProbe(true)
	b.recordProbe(false)
	if !b.Healthy() {
		t.Fatal("one failed probe must not mark a known backend down")
	}
	b.recordProbe(false)
	if b.Healthy() {
		t.Fatal("two failed probes must mark it down")
	}
	b.recordProbe(true)
	if !b.Healthy() {
		t.Fatal("a passing probe must restore it")
	}

	fresh := mustPool(t, []config.Backend{{Name: "a", URL: "http://a", Models: []string{"m"}}}).all[0]
	fresh.recordProbe(false)
	if fresh.Healthy() {
		t.Fatal("a failed first probe must count immediately")
	}
}

func TestCheckAllRunsInParallelAndMarksDown(t *testing.T) {
	slow := probeServer(t, func(w http.ResponseWriter, r *http.Request) { time.Sleep(200 * time.Millisecond) })
	bad := probeServer(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusBadGateway) })
	p := mustPool(t, []config.Backend{
		{Name: "s1", URL: slow, Models: []string{"m"}},
		{Name: "s2", URL: slow, Models: []string{"m"}},
		{Name: "bad", URL: bad, Models: []string{"m"}},
	})
	start := time.Now()
	p.CheckAll(context.Background(), "/")
	if d := time.Since(start); d > 350*time.Millisecond {
		t.Fatalf("probes ran serially: %v", d)
	}
	if p.all[2].Healthy() || !p.all[0].Healthy() {
		t.Fatalf("snapshot = %+v", p.Snapshot())
	}
}

func TestProbeTimeoutMarksDown(t *testing.T) {
	url := probeServer(t, func(w http.ResponseWriter, r *http.Request) { time.Sleep(300 * time.Millisecond) })
	p := mustPool(t, []config.Backend{{Name: "a", URL: url, Models: []string{"m"}}})
	p.HealthTimeout = 20 * time.Millisecond
	p.CheckAll(context.Background(), "/")
	if p.all[0].Healthy() {
		t.Fatal("a probe past its timeout must fail")
	}
}

func affinityPool(t *testing.T, n int) *Pool {
	t.Helper()
	var specs []config.Backend
	for i := range n {
		name := string(rune('a' + i))
		specs = append(specs, config.Backend{Name: name, URL: "http://" + name, Models: []string{"llama3"}})
	}
	return mustPool(t, specs, PrefixAffinity)
}

// turns returns the message prefixes of a conversation after n turns, each
// turn adding a reply and a new user message.
func turns(conversation string, n int) []uint64 {
	msgs := [][]byte{[]byte("user: " + conversation)}
	for i := 1; i < n; i++ {
		msgs = append(msgs, []byte("assistant: ok"), []byte(fmt.Sprintf("user: %s turn %d", conversation, i)))
	}
	return HashPrefixes(msgs)
}

func pick(t *testing.T, p *Pool, prefixes []uint64) *Backend {
	t.Helper()
	b, release, err := p.NextFor("llama3", prefixes)
	if err != nil {
		t.Fatalf("NextFor: %v", err)
	}
	release()
	return b
}

func TestHashPrefixesChain(t *testing.T) {
	a := HashPrefixes([][]byte{[]byte("x"), []byte("y")})
	b := HashPrefixes([][]byte{[]byte("x"), []byte("y"), []byte("z")})
	if a[0] != b[0] || a[1] != b[1] {
		t.Fatal("a shared prefix hashed differently")
	}
	if c := HashPrefixes([][]byte{[]byte("xy")}); c[0] == a[1] {
		t.Fatal("message boundaries are not part of the hash")
	}
}

func TestAffinityFollowsConversationAndSpreadsNewOnes(t *testing.T) {
	p := affinityPool(t, 4)
	seen := map[string]int{}
	for c := range 400 {
		conv := fmt.Sprintf("conversation-%d", c)
		first := pick(t, p, turns(conv, 1))
		for n := 2; n <= 4; n++ {
			if again := pick(t, p, turns(conv, n)); again != first {
				t.Fatalf("%s turn %d moved from %s to %s", conv, n, first.Name, again.Name)
			}
		}
		seen[first.Name]++
	}
	for name, n := range seen {
		if n < 60 || n > 140 { // 100 each on average
			t.Fatalf("new conversations spread unevenly: %s got %d of 400 (%v)", name, n, seen)
		}
	}
}

func TestAffinityOnlyMovesConversationsOfARemovedBackend(t *testing.T) {
	p := affinityPool(t, 4)
	before := map[string]*Backend{}
	for c := range 200 {
		conv := fmt.Sprintf("conversation-%d", c)
		before[conv] = pick(t, p, turns(conv, 1))
	}
	gone := p.all[2]
	gone.healthy.Store(false)
	for conv, was := range before {
		now := pick(t, p, turns(conv, 2))
		if was != gone && now != was {
			t.Fatalf("%s moved from healthy %s to %s", conv, was.Name, now.Name)
		}
		if now == gone {
			t.Fatalf("%s still routed to unhealthy %s", conv, gone.Name)
		}
	}
}

func TestAffinitySpillsWhenPreferredIsOverloaded(t *testing.T) {
	p := affinityPool(t, 2)
	preferred := pick(t, p, turns("hot", 1))
	preferred.inflight.Store(10) // the other backend is idle
	if b := pick(t, p, turns("hot", 2)); b == preferred {
		t.Fatalf("overloaded %s kept the conversation", preferred.Name)
	}
}

func TestAffinityWithoutPrefixesUsesLeastPending(t *testing.T) {
	p := affinityPool(t, 2)
	p.all[0].inflight.Store(5)
	for range 4 {
		if b := pick(t, p, nil); b != p.all[1] {
			t.Fatalf("request without messages went to busy backend %s", b.Name)
		}
	}
}

func TestPrefixMemoryForgetsOldestGeneration(t *testing.T) {
	var m prefixMemory
	m.add([]uint64{1})
	for i := range 2 * prefixMemorySize {
		m.add([]uint64{uint64(i + 10)})
	}
	if m.depth([]uint64{1}) != 0 {
		t.Fatal("entry survived two full generations")
	}
	if m.depth([]uint64{99, uint64(2*prefixMemorySize + 9)}) != 2 {
		t.Fatal("most recent entry forgotten")
	}
}
