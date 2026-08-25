# inferoute

[![CI](https://github.com/Rakshit-gen/inferoute/actions/workflows/ci.yml/badge.svg)](https://github.com/Rakshit-gen/inferoute/actions/workflows/ci.yml)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)

An OpenAI-compatible inference gateway written in Go: it round-robins
requests across multiple LLM backends serving the same model (e.g. several
Ollama or vLLM instances), health-checks them, fails over to the next
healthy one on error, streams SSE responses straight through unbuffered,
rate-limits per API key, and semantically caches non-streaming responses
using [NuclaDB](https://github.com/Rakshit-gen/NuclaDB).

## Why

The inference *engines* (vLLM, SGLang, llama.cpp, Ollama) are a solved,
crowded problem. The layer that routes to them — load balancing, failover,
caching, observability — is not, and it's infrastructure glue, which is
what Go is for.

## Quickstart

Run two Ollama instances serving the same model, so there's actually
something to route between:

```sh
OLLAMA_HOST=127.0.0.1:11434 ollama serve &
OLLAMA_HOST=127.0.0.1:11435 ollama serve &
ollama pull llama3
```

Build and run inferoute against them:

```sh
go build -o bin/inferouted ./cmd/inferouted
./bin/inferouted -config config.example.json
```

`config.example.json` already points at `:11434` and `:11435` for `llama3`.
Send it a request:

```sh
curl localhost:8081/v1/chat/completions \
  -d '{"model":"llama3","messages":[{"role":"user","content":"hi"}]}'
```

Run it a few more times and kill one `ollama serve` process — the next
request fails over to the survivor instead of erroring. `/metrics` serves
Prometheus metrics, `/healthz` is a liveness probe.

## Features

- **Routing + failover**: reads `model` from the request body, round-robins
  across healthy backends registered for it, retries the next one on a
  connection error or 5xx.
- **Streaming**: SSE responses are flushed to the client chunk-by-chunk as
  the backend produces them, not buffered.
- **Rate limiting** (`rate_limit` in config): per-API-key token bucket
  (`Authorization: Bearer <key>`, falling back to remote IP). 429 over the
  limit.
- **Semantic caching** (`cache` in config): non-streaming chat requests are
  embedded (via an Ollama-compatible `/api/embeddings` endpoint), looked up
  in NuclaDB, and served from cache on a close match; misses are stored
  after the backend responds. Streaming requests always bypass the cache —
  caching a partial SSE stream isn't implemented.
- **Metrics** (`/metrics`): request count by model/backend/status, request
  latency histogram by model, cache hit/miss/error counts. Point Prometheus
  or Grafana at it — no bundled dashboard; this is a backend-only tool.

## Semantic cache setup notes

Two things that will silently misbehave if you get them wrong, found by
actually running this against a real NuclaDB instance rather than trusting
the proto docs:

- **`nucladbd -dim` must match your embedding model's output size.**
  NuclaDB is started with a fixed vector dimension for the whole database;
  point `cache.embedding_model` at a model that produces vectors of that
  exact length, or inserts will be rejected.
- **`cache.max_distance`, not a similarity threshold.** NuclaDB's cosine
  "score" is `1 - cosine_similarity` — a *distance*, where `0` means
  identical and larger means further apart. `max_distance` is compared
  directly against that: a smaller number means a stricter match. This is
  the opposite of what "similarity threshold" naming would suggest, which
  is exactly the bug the first version of this cache had — verified and
  fixed by round-tripping identical and different prompts against a live
  NuclaDB instance and checking the actual scores returned, not by
  reasoning about it from the proto file.

## Status

Working end-to-end, verified against real binaries (not just unit tests):
routing, round-robin, health-checked failover, streaming passthrough,
per-key rate limiting, and NuclaDB-backed semantic caching with real
cache-hit/cache-miss behavior confirmed by request counts on the backend.

Not yet built: batched/async cache writes at scale, caching for streaming
responses, distributed rate limiting (current limiter state is
process-local — fine for one gateway instance, not for a fleet behind a
load balancer).

See tests in `internal/backend`, `internal/proxy`, `internal/cache`, and
`internal/ratelimit` for the behavior that's actually verified.
