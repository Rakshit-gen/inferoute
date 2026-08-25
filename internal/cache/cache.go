// Package cache implements semantic response caching backed by NuclaDB: it
// embeds the request's prompt text, looks up the nearest cached vector, and
// serves the stored response verbatim on a close-enough match.
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
	"encoding/json"
	"fmt"
	"hash/fnv"
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
}

type Cache struct {
	cfg    Config
	client *http.Client
}

func New(cfg Config) *Cache {
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

// Lookup embeds text and returns the cached response body for the nearest
// match if its distance is within MaxDistance.
func (c *Cache) Lookup(ctx context.Context, text string) ([]byte, bool, error) {
	vec, err := c.embed(ctx, text)
	if err != nil {
		return nil, false, fmt.Errorf("embedding for cache lookup: %w", err)
	}

	reqBody, _ := json.Marshal(map[string]any{
		"query": vec, "top_k": 1, "tenant_id": c.cfg.TenantID,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.cfg.NuclaDBAddr+"/v1/search", bytes.NewReader(reqBody))
	if err != nil {
		return nil, false, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, false, fmt.Errorf("searching cache: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(resp.Body)
		return nil, false, fmt.Errorf("searching cache: %s: %s", resp.Status, b)
	}

	var result struct {
		Matches []struct {
			Score    float32           `json:"score"`
			Metadata map[string]string `json:"metadata"`
		} `json:"matches"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, false, fmt.Errorf("decoding cache search response: %w", err)
	}
	if len(result.Matches) == 0 || result.Matches[0].Score > c.cfg.MaxDistance {
		return nil, false, nil
	}
	return []byte(result.Matches[0].Metadata["response"]), true, nil
}

// Store embeds text and upserts it into NuclaDB with response attached as
// metadata, keyed by a hash of text so identical prompts overwrite in place.
func (c *Cache) Store(ctx context.Context, text string, response []byte) error {
	vec, err := c.embed(ctx, text)
	if err != nil {
		return fmt.Errorf("embedding for cache store: %w", err)
	}

	reqBody, _ := json.Marshal(map[string]any{
		"id":        idFor(text),
		"values":    vec,
		"tenant_id": c.cfg.TenantID,
		"metadata":  map[string]string{"response": string(response), "prompt": text},
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

// idFor derives a NuclaDB vector id (a decimal uint64) from prompt text, so
// identical prompts overwrite the same vector in place.
func idFor(text string) string {
	h := fnv.New64a()
	h.Write([]byte(text))
	return strconv.FormatUint(h.Sum64(), 10)
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
		parts = append(parts, m.Content)
	}
	return strings.Join(parts, "\n")
}

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}
