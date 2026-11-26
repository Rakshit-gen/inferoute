package usage

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

func TestMeterReadsJSONBody(t *testing.T) {
	m := NewMeter("application/json")
	body := `{"choices":[{"message":{"content":"hi"}}],` + "\n" + `"usage":{"prompt_tokens":12,"completion_tokens":30,"total_tokens":42}}`
	m.Write([]byte(body[:20]))
	m.Write([]byte(body[20:]))
	got, ok := m.Tokens()
	if !ok || got != (Tokens{12, 30}) {
		t.Fatalf("got %+v %v", got, ok)
	}
}

func TestMeterReadsFinalSSEChunkAcrossWrites(t *testing.T) {
	stream := "data: {\"choices\":[{\"delta\":{\"content\":\"a\"}}],\"usage\":null}\n\n" +
		"data: {\"choices\":[],\"usage\":{\"prompt_tokens\":7,\"completion_tokens\":3}}\n\n" +
		"data: [DONE]\n\n"
	for _, size := range []int{1, 3, 17, len(stream)} {
		m := NewMeter("text/event-stream; charset=utf-8")
		for i := 0; i < len(stream); i += size {
			m.Write([]byte(stream[i:min(i+size, len(stream))]))
		}
		if got, ok := m.Tokens(); !ok || got != (Tokens{7, 3}) {
			t.Fatalf("write size %d: got %+v %v", size, got, ok)
		}
	}
}

func TestMeterWithoutUsage(t *testing.T) {
	for _, ct := range []string{"application/json", "text/event-stream"} {
		m := NewMeter(ct)
		m.Write([]byte("data: {\"choices\":[],\"usage\":null}\n\n"))
		if _, ok := m.Tokens(); ok {
			t.Fatalf("%s: found usage that was not reported", ct)
		}
	}
}

func TestMeterGivesUpOnOversizedBody(t *testing.T) {
	m := NewMeter("application/json")
	m.Write([]byte(`{"usage":{"prompt_tokens":1,"completion_tokens":1},"pad":"`))
	m.Write([]byte(strings.Repeat("x", maxMetered)))
	m.Write([]byte(`"}`))
	if _, ok := m.Tokens(); ok {
		t.Fatal("oversized body was metered")
	}
	if len(m.buf) != 0 {
		t.Fatal("oversized body is still held")
	}
}

func TestLocalBudgetCountsPerKeyAndRolls(t *testing.T) {
	b := NewLocal(time.Hour)
	b.Add("a", 10)
	b.Add("a", 5)
	b.Add("b", 1)
	if b.Used("a") != 15 || b.Used("b") != 1 || b.Used("c") != 0 {
		t.Fatalf("used a=%d b=%d c=%d", b.Used("a"), b.Used("b"), b.Used("c"))
	}
	b.current-- // pretend the counts belong to the previous window
	if b.Used("a") != 0 {
		t.Fatal("counts survived into a new window")
	}
	if r := b.Resets(); !r.After(time.Now()) || time.Until(r) > time.Hour {
		t.Fatalf("reset time %v is not within the current hour window", r)
	}
}

func TestRedisBudgetIsShared(t *testing.T) {
	rdb := redis.NewClient(&redis.Options{Addr: "localhost:6379"})
	defer rdb.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	if err := rdb.Ping(ctx).Err(); err != nil {
		t.Skipf("no redis reachable at localhost:6379: %v", err)
	}
	key := "test-" + time.Now().Format(time.RFC3339Nano)
	one, err := NewRedis("localhost:6379", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	two, _ := NewRedis("redis://localhost:6379", time.Hour)
	one.Add(key, 100)
	two.Add(key, 20)
	if got := one.Used(key); got != 120 {
		t.Fatalf("instances do not share the count: %d", got)
	}
}

func TestRedisBudgetFallsBackWhenDown(t *testing.T) {
	b, err := NewRedis("localhost:1", time.Hour) // nothing listens there
	if err != nil {
		t.Fatal(err)
	}
	b.Add("k", 9)
	if got := b.Used("k"); got != 9 {
		t.Fatalf("outage lost the count: %d", got)
	}
}

func TestMeterIgnoresUsageInsideStrings(t *testing.T) {
	m := NewMeter("application/json")
	m.Write([]byte(`{"choices":[{"message":{"content":"the \"usage\": field"}}], "usage" : {"prompt_tokens":3,"completion_tokens":4}}`))
	if got, ok := m.Tokens(); !ok || got != (Tokens{3, 4}) {
		t.Fatalf("got %+v %v", got, ok)
	}
	m = NewMeter("application/json")
	m.Write([]byte(`{"choices":[{"message":{"content":"the \"usage\": {\"prompt_tokens\":9}"}}]}`))
	if _, ok := m.Tokens(); ok {
		t.Fatal("usage quoted in content was metered")
	}
}
