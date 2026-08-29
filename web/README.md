# inferoute · web

A static multi-page site for a running [inferoute](../) gateway. It talks to
`inferouted`'s HTTP API from the browser, nothing server-side.

- **/** — landing: what inferoute does, the dispatch path, quickstart.
- **/dashboard** — the dispatch path (ingress → rate limit → cache →
  load balancer → backend), live backend health, throughput / latency / error /
  cache-hit tiles, status-code breakdown, and the model routing table. Polls
  `/v1/backends`, `/v1/config`, and `/metrics`.
- **/playground** — send one completion through the gateway and see which
  backend served it (`X-Inferoute-Backend`) and whether it was a cache hit
  (`X-Inferoute-Cache`), with the real route drawn out.
- **/docs** — the HTTP surface and config reference.

## Run

```sh
npm install
cp .env.example .env.local   # point NEXT_PUBLIC_INFEROUTE_URL at your gateway
npm run dev                  # http://localhost:3002
```

The gateway's `cors_origins` must include this dashboard's origin (or be
`["*"]`, the default).

## Build / deploy

```sh
npm run build   # static export to ./out
```

Deploy `out/` to any static host. On Vercel: set the project root to `web/`,
framework preset Next.js, and add `NEXT_PUBLIC_INFEROUTE_URL` as an env var.
