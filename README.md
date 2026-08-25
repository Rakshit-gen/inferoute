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

You'll get back whatever Ollama returns, unmodified — inferoute just picked
which of the two instances handled it:

```json
{"model":"llama3","message":{"role":"assistant","content":"..."}, ...}
```

Run it a few more times and kill one `ollama serve` process — the next
request fails over to the survivor instead of erroring. `/metrics` serves
Prometheus metrics, `/healthz` is a liveness probe.

## CLI

The binary is `inferouted`, and it has exactly one flag:

```sh
./bin/inferouted -config path/to/config.json   # defaults to ./config.json
./bin/inferouted -h                            # prints usage and exits
```

There's no subcommand or interactive mode — it's a daemon: point it at a
config file, it starts listening, `Ctrl-C` (or `SIGTERM`) shuts it down
cleanly. Everything else is controlled through the config file below or by
calling the HTTP API it serves.

## Configuration

Everything lives in one JSON file (see `config.example.json` for a working
one). Plain-English rundown of each section:

| Field | What it does |
|---|---|
| `listen_addr` | The address inferoute itself listens on, e.g. `:8081`. |
| `health_check_path`, `health_check_interval` | Which path to `GET` on each backend to check it's alive, and how often. |
| `backends` | The list of servers to route to. Each entry is `{name, url, models}` — `models` is the list of model names that backend can serve; two backends listing the same model get load-balanced between. |
| `rate_limit.enabled` | Turn per-API-key rate limiting on or off. Off by default. |
| `rate_limit.requests_per_second`, `.burst` | Steady-state rate and how many requests can burst above it before a caller starts getting `429`s. |
| `cache.enabled` | Turn semantic response caching on or off. Off by default, and requires a running [NuclaDB](https://github.com/Rakshit-gen/NuclaDB) instance. |
| `cache.nucladb_addr` | Where that NuclaDB instance is. |
| `cache.embedding_backend_addr`, `.embedding_model` | Which Ollama-compatible server and model to use to turn a prompt into a vector for cache lookups. |
| `cache.max_distance` | How close a cached prompt has to be to count as a hit — see the note below, the naming here is easy to get backwards. |
| `cache.tenant_id` | The NuclaDB tenant inferoute's cache vectors are stored under. |

Every field has a sane default except `backends`, which is required.

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
