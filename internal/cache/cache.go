// Package cache implements semantic response caching backed by NuclaDB: it
// embeds the request's prompt text, looks up the nearest cached vector, and
// serves the stored response verbatim on a close-enough match. Both
// non-streaming and streaming (SSE) responses are cached; a streamed hit is
// replayed as one write of the originally captured bytes rather than
// re-created chunk by chunk.
//
// The embedding model's output dimensionality must match the dimension the
// target NuclaDB instance was started with (nucladbd -dim); this package
// doesn't validate that, NuclaDB's insert call will just reject mismatched
// vectors.
//
// NuclaDB's cosine-metric "score" is a distance (1 - cosine similarity), not
// a similarity: 0 means identical, larger means further apart. MaxDistance
// is compared against that distance directly — don't treat it as a
// similarity threshold.
package cache

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	NuclaDBAddr          string
	EmbeddingBackendAddr string
	EmbeddingModel       string
	MaxDistance          float32
	TenantID             string
	LookupTimeout        time.Duration // 0 means 2 seconds
}

type Cache struct {
	cfg    Config
	client *http.Client
}

func New(cfg Config) *Cache {
	if cfg.LookupTimeout <= 0 {
		cfg.LookupTimeout = 2 * time.Second
	}
	return &Cache{cfg: cfg, client: &http.Client{Timeout: 30 * time.Second}}
}

// EnsureTenant creates the cache's NuclaDB tenant. It's safe to call every
// startup: an "already exists" error from NuclaDB is not surfaced.
func (c *Cache) EnsureTenant(ctx context.Context) error {
	body, _ := json.Marshal(map[string]string{"tenant_id": c.cfg.TenantID})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.cfg.NuclaDBAddr+"/v1/tenants", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("creating cache tenant: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(resp.Body)
		// NuclaDB reports an existing tenant as 400 InvalidArgument, not 409,
		// so "already exists" is the only reliable signal to ignore here.
		if resp.StatusCode == http.StatusConflict || strings.Contains(string(b), "already exists") {
			return nil
		}
		return fmt.Errorf("creating cache tenant: %s: %s", resp.Status, b)
	}
	return nil
}

// searchDepth is how many neighbours are checked for one in the right scope.
const searchDepth = 10

// Scope derives the partition a request's entries live in. Only requests
// with the same scope may share a cached response.
func Scope(parts ...string) string {
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(sum[:])
}

// Result is the outcome of a Lookup.
type Result struct {
	Hit         bool
	Body        []byte
	ContentType string // text/event-stream for a streamed response
}

// Lookup embeds text and returns the nearest entry in scope whose distance
// is within MaxDistance.
func (c *Cache) Lookup(ctx context.Context, scope, text string) (Result, error) {
	ctx, cancel := context.WithTimeout(ctx, c.cfg.LookupTimeout)
	defer cancel()
	vec, err := c.embed(ctx, text)
	if err != nil {
		return Result{}, fmt.Errorf("embedding for cache lookup: %w", err)
	}

	reqBody, _ := json.Marshal(map[string]any{
		"query": vec, "top_k": searchDepth, "tenant_id": c.cfg.TenantID,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.cfg.NuclaDBAddr+"/v1/search", bytes.NewReader(reqBody))
	if err != nil {
		return Result{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.client.Do(req)
	if err != nil {
		return Result{}, fmt.Errorf("searching cache: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(resp.Body)
		return Result{}, fmt.Errorf("searching cache: %s: %s", resp.Status, b)
	}

	var result struct {
		Matches []struct {
			Score    float32           `json:"score"`
			Metadata map[string]string `json:"metadata"`
		} `json:"matches"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return Result{}, fmt.Errorf("decoding cache search response: %w", err)
	}
	for _, m := range result.Matches {
		if m.Score > c.cfg.MaxDistance || m.Metadata["scope"] != scope || m.Metadata["response"] == "" {
			continue
		}
		ct := m.Metadata["content_type"]
		if ct == "" {
			ct = "application/json"
		}
		return Result{Hit: true, Body: []byte(m.Metadata["response"]), ContentType: ct}, nil
	}
	return Result{}, nil
}

// Store embeds text and upserts it into NuclaDB with response and
// contentType attached as metadata, keyed so an identical prompt in the same
// scope overwrites in place.
func (c *Cache) Store(ctx context.Context, scope, text string, response []byte, contentType string) error {
	vec, err := c.embed(ctx, text)
	if err != nil {
		return fmt.Errorf("embedding for cache store: %w", err)
	}

	reqBody, _ := json.Marshal(map[string]any{
		"id":        idFor(scope, text),
		"values":    vec,
		"tenant_id": c.cfg.TenantID,
		"metadata":  map[string]string{"response": string(response), "prompt": text, "content_type": contentType, "scope": scope},
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.cfg.NuclaDBAddr+"/v1/vectors", bytes.NewReader(reqBody))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("storing in cache: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("storing in cache: %s: %s", resp.Status, b)
	}
	return nil
}

// idFor derives a NuclaDB vector id (a decimal uint64) from scope and text.
func idFor(scope, text string) string {
	sum := sha256.Sum256([]byte(scope + "\x00" + text))
	return strconv.FormatUint(binary.BigEndian.Uint64(sum[:8]), 10)
}

// embed calls an Ollama-compatible /api/embeddings endpoint.
func (c *Cache) embed(ctx context.Context, text string) ([]float32, error) {
	reqBody, _ := json.Marshal(map[string]string{"model": c.cfg.EmbeddingModel, "prompt": text})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.cfg.EmbeddingBackendAddr+"/api/embeddings", bytes.NewReader(reqBody))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("%s: %s", resp.Status, b)
	}

	var out struct {
		Embedding []float32 `json:"embedding"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	if len(out.Embedding) == 0 {
		return nil, fmt.Errorf("embedding backend returned an empty vector")
	}
	return out.Embedding, nil
}

// PromptText joins the content of chat messages into the text that gets
// embedded and used as the cache key.
func PromptText(messages []Message) string {
	parts := make([]string, 0, len(messages))
	for _, m := range messages {
		parts = append(parts, m.Role+": "+m.Content)
	}
	return strings.Join(parts, "\n")
}

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// ParseMessages decodes a chat "messages" array for caching. ok is false when
// any message is more than text (images, tool calls), since the embedded
// prompt could not represent it and a match would be wrong.
func ParseMessages(raw json.RawMessage) (messages []Message, ok bool) {
	var parsed []struct {
		Role       string          `json:"role"`
		Content    json.RawMessage `json:"content"`
		ToolCalls  json.RawMessage `json:"tool_calls"`
		ToolCallID string          `json:"tool_call_id"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil || len(parsed) == 0 {
		return nil, false
	}
	for _, m := range parsed {
		if len(m.ToolCalls) > 0 || m.ToolCallID != "" {
			return nil, false
		}
		text, ok := textContent(m.Content)
		if !ok {
			return nil, false
		}
		messages = append(messages, Message{Role: m.Role, Content: text})
	}
	return messages, true
}

// textContent accepts a string or an array made up only of text parts.
func textContent(raw json.RawMessage) (string, bool) {
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s, true
	}
	var parts []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(raw, &parts); err != nil {
		return "", false
	}
	texts := make([]string, 0, len(parts))
	for _, p := range parts {
		if p.Type != "text" {
			return "", false
		}
		texts = append(texts, p.Text)
	}
	return strings.Join(texts, "\n"), true
}
