import Link from 'next/link'
import { DispatchStrip } from '@/components/dispatch-strip'

const GITHUB = 'https://github.com/Rakshit-gen/inferoute'

const capabilities = [
  {
    k: 'route',
    title: 'Routing + failover',
    body: 'Reads the model from the request body, round-robins across the healthy backends registered for it, retries the next one on a connection error or 5xx.',
  },
  {
    k: 'route',
    title: 'Streaming passthrough',
    body: 'SSE responses are flushed to the caller chunk by chunk as the backend produces them — never buffered.',
  },
  {
    k: 'route',
    title: 'Per-key rate limiting',
    body: 'Token bucket keyed on the API key (or client IP). In-process by default; point it at Redis to share the limit across a fleet.',
  },
  {
    k: 'cache',
    title: 'Semantic cache',
    body: 'Chat prompts — streaming included — are embedded, looked up in NuclaDB, and served from cache on a close match. A cached stream is replayed byte for byte.',
  },
  {
    k: 'route',
    title: 'Config hot-reload',
    body: 'SIGHUP reloads the backend list and model aliases from the config file without dropping in-flight requests.',
  },
  {
    k: 'cache',
    title: 'Prometheus metrics',
    body: 'Request volume and latency by model and backend, cache hit / miss / error counts, on /metrics. This dashboard reads them live.',
  },
]

export default function Landing() {
  return (
    <div className="space-y-16">
      <section className="pt-8">
        <p className="eyebrow">OpenAI-compatible inference gateway</p>
        <h1 className="mt-3 font-display text-4xl font-bold leading-[1.1] tracking-tight sm:text-5xl">
          Route, cache, and rate-limit
          <br />
          <span className="text-route">your inference backends</span>
        </h1>
        <p className="mt-4 max-w-xl text-ink-dim">
          inferoute sits in front of your Ollama, vLLM, or hosted LLM endpoints and load-balances
          across them, health-checks them, fails over on error, and semantically caches responses.
          One binary, one JSON config.
        </p>
        <div className="mt-6 flex flex-wrap gap-3">
          <Link
            href="/dashboard"
            className="rounded bg-route px-4 py-2 font-display text-sm font-semibold text-void"
          >
            Open dashboard
          </Link>
          <Link
            href="/docs"
            className="rounded border border-line px-4 py-2 font-display text-sm font-semibold text-ink hover:border-ink-dim"
          >
            Read the docs
          </Link>
          <a
            href={GITHUB}
            target="_blank"
            rel="noreferrer"
            className="rounded border border-line px-4 py-2 font-display text-sm font-semibold text-ink-dim hover:border-ink-dim hover:text-ink"
          >
            GitHub ↗
          </a>
        </div>
      </section>

      <section>
        <DispatchStrip rateLimit cache backends={['ollama-1', 'ollama-2']} />
        <p className="mt-2 text-xs text-ink-dim">
          The path every request travels. Left to right is request flow — the dashboard lights up
          the segment that actually handled each call.
        </p>
      </section>

      <section>
        <h2 className="eyebrow mb-4">what it does</h2>
        <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
          {capabilities.map((c) => (
            <div key={c.title} className="panel p-4">
              <div className="flex items-center gap-2">
                <span
                  className="led"
                  style={{ background: c.k === 'cache' ? 'var(--cache)' : 'var(--route)' }}
                />
                <h3 className="font-display text-sm font-semibold">{c.title}</h3>
              </div>
              <p className="mt-2 text-sm text-ink-dim">{c.body}</p>
            </div>
          ))}
        </div>
      </section>

      <section>
        <h2 className="eyebrow mb-4">quickstart</h2>
        <pre className="panel overflow-x-auto p-4 font-mono text-xs leading-relaxed text-ink-dim">
{`# install (needs Go), grab the sample config, run
curl -fsSL https://raw.githubusercontent.com/Rakshit-gen/inferoute/main/install.sh | sh
curl -O https://raw.githubusercontent.com/Rakshit-gen/inferoute/main/config.example.json
inferouted -config config.example.json

# send it a request
curl localhost:8081/v1/chat/completions \\
  -d '{"model":"llama3","messages":[{"role":"user","content":"hi"}]}'`}
        </pre>
        <p className="mt-2 text-xs text-ink-dim">
          Then point this dashboard at it with <span className="font-mono text-ink">NEXT_PUBLIC_INFEROUTE_URL</span>.
        </p>
      </section>

      <footer className="border-t border-line pt-6 text-xs text-ink-dim">
        inferoute · <a href={GITHUB} className="hover:text-ink">github.com/Rakshit-gen/inferoute</a>
      </footer>
    </div>
  )
}
