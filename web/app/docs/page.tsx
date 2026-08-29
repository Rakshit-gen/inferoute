import Link from 'next/link'

const GITHUB = 'https://github.com/Rakshit-gen/inferoute'

function H({ children }: { children: React.ReactNode }) {
  return <h2 className="mt-10 font-display text-lg font-semibold tracking-tight">{children}</h2>
}

function Code({ children }: { children: React.ReactNode }) {
  return <code className="rounded bg-panel-2 px-1.5 py-0.5 font-mono text-[0.85em] text-ink">{children}</code>
}

const endpoints: [string, string][] = [
  ['POST /v1/chat/completions', 'The proxy. Reads "model" from the body, resolves aliases, picks a healthy backend, streams the response back. Cache is consulted first when enabled.'],
  ['GET /v1/backends', 'JSON array of every backend: name, url, models, healthy.'],
  ['GET /v1/config', 'Model aliases, health-check interval, and whether rate limiting / cache are on.'],
  ['GET /metrics', 'Prometheus text: request volume + latency by model and backend, cache hit / miss / error.'],
  ['GET /healthz', 'Liveness probe. Always 200 while the process is up.'],
  ['GET /docs', "inferoute's own built-in documentation page, served from the binary."],
]

const headers: [string, string][] = [
  ['X-Inferoute-Backend', 'Name of the backend that served the response, or "cache" on a cache hit.'],
  ['X-Inferoute-Cache', 'Present and set to "hit" when the response came from the semantic cache.'],
]

const configFields: [string, string][] = [
  ['listen_addr', 'Address inferoute listens on. Default :8081.'],
  ['health_check_path / health_check_interval', 'Path to GET on each backend to check liveness, and how often.'],
  ['backends', 'Array of { name, url, models, path_prefix?, api_key? }. Two backends listing the same model get load-balanced. path_prefix is prepended to the forwarded path; api_key overrides the caller\'s Authorization for that backend.'],
  ['rate_limit', '{ enabled, requests_per_second, burst, redis_addr? }. Token bucket per API key. Set redis_addr to share the limit across instances.'],
  ['cache', '{ enabled, nucladb_addr, embedding_backend_addr, embedding_model, max_distance, tenant_id }. Needs a running NuclaDB. max_distance is a distance, not a similarity — smaller is stricter.'],
  ['model_aliases', 'Map a requested model name to one your backends actually serve, e.g. {"gpt-4": "llama3"}.'],
  ['cors_origins', 'Browser origins allowed to call the API (this dashboard). ["*"] allows any; default ["*"].'],
  ['api_keys', 'Allowlist of bearer tokens for POST /v1/chat/completions. Non-empty → requests without a listed Authorization: Bearer <key> get 401. Empty (default) leaves the proxy open. Reloaded on SIGHUP. Introspection and /metrics are not gated.'],
]

export default function Docs() {
  return (
    <article className="max-w-3xl">
      <p className="eyebrow">reference</p>
      <h1 className="mt-3 font-display text-3xl font-bold tracking-tight">Documentation</h1>
      <p className="mt-3 text-ink-dim">
        inferoute is an OpenAI-compatible inference gateway: it load-balances and fails over across
        LLM backends serving the same model, rate-limits per key, and semantically caches responses.
        The full source and install steps are on{' '}
        <a href={GITHUB} className="text-route hover:underline">GitHub</a>.
      </p>

      <H>HTTP endpoints</H>
      <dl className="mt-3 space-y-3">
        {endpoints.map(([name, desc]) => (
          <div key={name} className="panel p-3">
            <dt className="font-mono text-sm text-ink">{name}</dt>
            <dd className="mt-1 text-sm text-ink-dim">{desc}</dd>
          </div>
        ))}
      </dl>

      <H>Response headers</H>
      <dl className="mt-3 space-y-3">
        {headers.map(([name, desc]) => (
          <div key={name} className="panel p-3">
            <dt className="font-mono text-sm text-cache">{name}</dt>
            <dd className="mt-1 text-sm text-ink-dim">{desc}</dd>
          </div>
        ))}
      </dl>
      <p className="mt-2 text-sm text-ink-dim">
        The <Link href="/playground" className="text-route hover:underline">playground</Link> sends a
        real request and shows both, along with the route it took.
      </p>

      <H>Config file</H>
      <p className="mt-2 text-sm text-ink-dim">
        Everything is one JSON file passed with <Code>-config</Code>. Every field has a sane default
        except <Code>backends</Code>.
      </p>
      <dl className="mt-3 space-y-3">
        {configFields.map(([name, desc]) => (
          <div key={name} className="panel p-3">
            <dt className="font-mono text-sm text-ink">{name}</dt>
            <dd className="mt-1 text-sm text-ink-dim">{desc}</dd>
          </div>
        ))}
      </dl>

      <H>Reloading config</H>
      <p className="mt-2 text-sm text-ink-dim">
        A <Code>SIGHUP</Code> reloads <Code>backends</Code> and <Code>model_aliases</Code> without
        restarting or dropping in-flight requests. <Code>rate_limit</Code> and <Code>cache</Code>{' '}
        changes need a restart.
      </p>
      <pre className="panel mt-3 overflow-x-auto p-3 font-mono text-xs text-ink-dim">
        kill -HUP $(pgrep inferouted)
      </pre>

      <H>Running this dashboard</H>
      <p className="mt-2 text-sm text-ink-dim">
        The dashboard is a static site that reads inferoute&apos;s HTTP API from the browser. Set{' '}
        <Code>NEXT_PUBLIC_INFEROUTE_URL</Code> to your gateway&apos;s base URL and make sure its{' '}
        <Code>cors_origins</Code> allows the dashboard&apos;s origin.
      </p>
    </article>
  )
}
