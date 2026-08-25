package cache

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// fakeNuclaDB is just enough of the NuclaDB REST API for Lookup/Store to
// exercise real HTTP round trips against: an in-memory single-vector store
// with cosine-free "similarity" driven entirely by the test.
type fakeNuclaDB struct {
	stored map[string]struct {
		values   []float32
		metadata map[string]string
	}
	nextScore float32 // score to return from the next /v1/search call
}

func newFakeNuclaDB() *fakeNuclaDB {
	return &fakeNuclaDB{stored: map[string]struct {
		values   []float32
		metadata map[string]string
	}{}}
}

func (f *fakeNuclaDB) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/tenants", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	mux.HandleFunc("POST /v1/vectors", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			ID       string            `json:"id"`
			Values   []float32         `json:"values"`
			Metadata map[string]string `json:"metadata"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		f.stored[body.ID] = struct {
			values   []float32
			metadata map[string]string
		}{body.Values, body.Metadata}
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("POST /v1/search", func(w http.ResponseWriter, r *http.Request) {
		if len(f.stored) == 0 {
			json.NewEncoder(w).Encode(map[string]any{"matches": []any{}})
			return
		}
		var last string
		for id := range f.stored {
			last = id
		}
		json.NewEncoder(w).Encode(map[string]any{
			"matches": []map[string]any{{
				"score":    f.nextScore,
				"metadata": f.stored[last].metadata,
			}},
		})
	})
	return mux
}

func fakeEmbedder(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"embedding": []float32{0.1, 0.2, 0.3}})
	}))
}

func TestStoreThenLookupHit(t *testing.T) {
	db := newFakeNuclaDB()
	dbSrv := httptest.NewServer(db.handler())
	defer dbSrv.Close()
	embed := fakeEmbedder(t)
	defer embed.Close()

	c := New(Config{
		NuclaDBAddr: dbSrv.URL, EmbeddingBackendAddr: embed.URL,
		EmbeddingModel: "test-embed", MaxDistance: 0.1, TenantID: "t1",
	})

	if err := c.Store(context.Background(), "what is go", []byte(`{"answer":"a language"}`), "application/json"); err != nil {
		t.Fatalf("Store: %v", err)
	}

	db.nextScore = 0.01 // near-zero distance: essentially the same vector
	got, ct, hit, err := c.Lookup(context.Background(), "what is go")
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if !hit {
		t.Fatal("expected cache hit within max distance")
	}
	if string(got) != `{"answer":"a language"}` {
		t.Fatalf("unexpected cached body: %s", got)
	}
	if ct != "application/json" {
		t.Fatalf("unexpected content type: %s", ct)
	}
}

func TestStoreThenLookupHitStreaming(t *testing.T) {
	db := newFakeNuclaDB()
	dbSrv := httptest.NewServer(db.handler())
	defer dbSrv.Close()
	embed := fakeEmbedder(t)
	defer embed.Close()

	c := New(Config{
		NuclaDBAddr: dbSrv.URL, EmbeddingBackendAddr: embed.URL,
		EmbeddingModel: "test-embed", MaxDistance: 0.1, TenantID: "t1",
	})

	sse := "data: chunk-0\n\ndata: chunk-1\n\n"
	if err := c.Store(context.Background(), "stream me", []byte(sse), "text/event-stream"); err != nil {
		t.Fatalf("Store: %v", err)
	}

	db.nextScore = 0.01
	got, ct, hit, err := c.Lookup(context.Background(), "stream me")
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if !hit {
		t.Fatal("expected cache hit within max distance")
	}
	if string(got) != sse {
		t.Fatalf("unexpected cached body: %s", got)
	}
	if ct != "text/event-stream" {
		t.Fatalf("unexpected content type: %s", ct)
	}
}

func TestLookupMissBeyondMaxDistance(t *testing.T) {
	db := newFakeNuclaDB()
	dbSrv := httptest.NewServer(db.handler())
	defer dbSrv.Close()
	embed := fakeEmbedder(t)
	defer embed.Close()

	c := New(Config{
		NuclaDBAddr: dbSrv.URL, EmbeddingBackendAddr: embed.URL,
		EmbeddingModel: "test-embed", MaxDistance: 0.05, TenantID: "t1",
	})
	c.Store(context.Background(), "unrelated prompt", []byte(`{"a":1}`), "application/json")

	db.nextScore = 1.2 // large distance: an unrelated vector
	_, _, hit, err := c.Lookup(context.Background(), "something else entirely")
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if hit {
		t.Fatal("expected miss beyond max distance")
	}
}

func TestPromptTextJoinsMessages(t *testing.T) {
	got := PromptText([]Message{{Role: "user", Content: "hi"}, {Role: "assistant", Content: "hello"}})
	want := "hi\nhello"
	if got != want {
		t.Fatalf("PromptText = %q, want %q", got, want)
	}
}
