// Package usage reads the token counts backends report in OpenAI-compatible
// responses and keeps per-caller token budgets over fixed time windows.
package usage

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/redis/go-redis/v9"
)

// Tokens is what one response consumed.
type Tokens struct {
	Prompt, Completion int64
}

// maxMetered bounds what a Meter holds: a whole non-streaming body, or one
// line of a stream. Larger ones go unmetered.
const maxMetered = 1 << 20

// Meter finds the "usage" object in a response as it passes through. For an
// SSE stream it reads line by line and keeps only the current line, since
// usage arrives in the final chunk; for anything else it keeps the body.
type Meter struct {
	sse      bool
	buf      []byte
	overflow bool // the current line (or body) passed maxMetered
	tokens   Tokens
	found    bool
}

// NewMeter returns a Meter for a response with the given Content-Type.
func NewMeter(contentType string) *Meter {
	return &Meter{sse: strings.HasPrefix(contentType, "text/event-stream")}
}

func (m *Meter) Write(p []byte) {
	if !m.sse {
		if !m.overflow && len(m.buf)+len(p) <= maxMetered {
			m.buf = append(m.buf, p...)
		} else {
			m.overflow, m.buf = true, nil
		}
		return
	}
	for len(p) > 0 {
		i := bytes.IndexByte(p, '\n')
		chunk := p
		if i >= 0 {
			chunk = p[:i]
		}
		if !m.overflow && len(m.buf)+len(chunk) <= maxMetered {
			m.buf = append(m.buf, chunk...)
		} else {
			m.overflow, m.buf = true, m.buf[:0]
		}
		if i < 0 {
			return
		}
		if !m.overflow {
			m.line(m.buf)
		}
		m.buf, m.overflow, p = m.buf[:0], false, p[i+1:]
	}
}

// line handles one SSE line.
func (m *Meter) line(l []byte) {
	data, ok := bytes.CutPrefix(l, []byte("data:"))
	if ok && bytes.Contains(data, []byte(`"usage"`)) {
		m.parse(data)
	}
}

// parse decodes the object after the last "usage" key in b rather than the
// whole document. A quote inside a JSON string is always escaped, so the
// match is a real key; OpenAI-compatible servers put usage last.
func (m *Meter) parse(b []byte) {
	i := bytes.LastIndex(b, []byte(`"usage"`))
	if i < 0 {
		return
	}
	rest := bytes.TrimLeft(b[i+len(`"usage"`):], " \t\r\n")
	rest, ok := bytes.CutPrefix(rest, []byte(":"))
	if !ok {
		return
	}
	var u *struct {
		Prompt     int64 `json:"prompt_tokens"`
		Completion int64 `json:"completion_tokens"`
	}
	if json.NewDecoder(bytes.NewReader(rest)).Decode(&u) == nil && u != nil {
		m.tokens, m.found = Tokens{u.Prompt, u.Completion}, true
	}
}

// Tokens returns the usage the response reported, if it reported any.
func (m *Meter) Tokens() (Tokens, bool) {
	if !m.sse && !m.overflow && len(m.buf) > 0 {
		m.parse(m.buf)
		m.buf = nil
	}
	return m.tokens, m.found
}

// Budget counts the tokens each caller spends in fixed windows aligned to
// the Unix epoch, so every gateway instance agrees on when a window resets.
type Budget interface {
	// Used returns what key has spent in the current window.
	Used(key string) int64
	// Add records n more tokens for key.
	Add(key string, n int64)
	// Resets returns when the current window ends.
	Resets() time.Time
}

func windowOf(t time.Time, window time.Duration) int64 { return t.UnixNano() / int64(window) }

func windowEnd(t time.Time, window time.Duration) time.Time {
	return time.Unix(0, (windowOf(t, window)+1)*int64(window))
}

// Local is a Budget for a single gateway instance.
type Local struct {
	window time.Duration

	mu      sync.Mutex
	current int64 // window index the counts belong to
	used    map[string]int64
}

func NewLocal(window time.Duration) *Local {
	return &Local{window: window, used: map[string]int64{}}
}

// roll forgets the previous window's counts once a new window starts.
func (l *Local) roll() {
	if w := windowOf(time.Now(), l.window); w != l.current {
		l.current, l.used = w, map[string]int64{}
	}
}

func (l *Local) Used(key string) int64 {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.roll()
	return l.used[key]
}

func (l *Local) Add(key string, n int64) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.roll()
	l.used[key] += n
}

func (l *Local) Resets() time.Time { return windowEnd(time.Now(), l.window) }

// Redis is a Budget shared by every gateway instance pointed at the same
// Redis. While Redis is unreachable it counts per instance instead, so an
// outage neither blocks traffic nor removes the budget.
type Redis struct {
	rdb    *redis.Client
	window time.Duration

	local     *Local
	downUntil atomic.Int64 // unix nanos before which Redis is not retried
}

const (
	redisCallTimeout = 100 * time.Millisecond
	redisRetryAfter  = time.Second
)

// NewRedis connects to addr, either host:port or a redis:// or rediss:// URL.
func NewRedis(addr string, window time.Duration) (*Redis, error) {
	opts := &redis.Options{Addr: addr}
	if strings.Contains(addr, "://") {
		var err error
		if opts, err = redis.ParseURL(addr); err != nil {
			// The parser's error can quote the URL, which holds the password.
			return nil, errors.New("usage.redis_addr is not a valid redis URL")
		}
	}
	opts.DialTimeout, opts.ReadTimeout, opts.WriteTimeout = redisCallTimeout, redisCallTimeout, redisCallTimeout
	opts.MaxRetries = -1 // the fallback is the retry
	return &Redis{rdb: redis.NewClient(opts), window: window, local: NewLocal(window)}, nil
}

func (r *Redis) key(key string) string {
	return "inferoute:tokens:" + key + ":" + strconv.FormatInt(windowOf(time.Now(), r.window), 10)
}

// call runs f against Redis unless it recently failed, and reports whether
// it succeeded.
func (r *Redis) call(f func(ctx context.Context) error) bool {
	now := time.Now()
	wasDown := r.downUntil.Load()
	if now.UnixNano() < wasDown {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), redisCallTimeout)
	defer cancel()
	if err := f(ctx); err != nil && !errors.Is(err, redis.Nil) {
		if wasDown == 0 {
			log.Printf("inferoute: redis token budget unavailable, counting per instance: %v", err)
		}
		r.downUntil.Store(now.Add(redisRetryAfter).UnixNano())
		return false
	}
	if wasDown != 0 && r.downUntil.CompareAndSwap(wasDown, 0) {
		log.Printf("inferoute: redis token budget recovered")
	}
	return true
}

func (r *Redis) Used(key string) int64 {
	var n int64
	if r.call(func(ctx context.Context) error {
		v, err := r.rdb.Get(ctx, r.key(key)).Int64()
		n = v
		return err
	}) {
		return n
	}
	return r.local.Used(key)
}

func (r *Redis) Add(key string, n int64) {
	k := r.key(key)
	if !r.call(func(ctx context.Context) error {
		pipe := r.rdb.TxPipeline()
		pipe.IncrBy(ctx, k, n)
		pipe.Expire(ctx, k, 2*r.window)
		_, err := pipe.Exec(ctx)
		return err
	}) {
		r.local.Add(key, n)
	}
}

func (r *Redis) Resets() time.Time { return windowEnd(time.Now(), r.window) }
