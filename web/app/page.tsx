import Link from 'next/link'
import { DispatchSim } from '@/components/dispatch-sim'
import { Reveal } from '@/components/reveal'
import { Logo } from '@/components/logo'

const GITHUB = 'https://github.com/Rakshit-gen/inferoute'

const flow = [
  {
    n: '01',
    k: 'route',
    title: 'Ingress',
    body: 'A caller sends an OpenAI-shaped POST /v1/chat/completions. inferoute reads the model field from the body. Nothing else about the request needs to change.',
  },
  {
    n: '02',
    k: 'warn',
    title: 'Rate limit',
    body: 'A token bucket keyed on the API key (or client IP) decides whether this request gets through. In-process by default; back it with Redis to share one budget across a fleet.',
  },
  {
    n: '03',
    k: 'cache',
    title: 'Semantic cache',
    body: 'If caching is on, the prompt is embedded and looked up in NuclaDB. A close-enough match is served straight from cache (a streamed response is replayed chunk for chunk).',
  },
  {
    n: '04',
    k: 'route',
    title: 'Route + failover',
    body: 'On a miss, inferoute load-balances across the healthy backends registered for that model (round-robin, least-pending, or weighted). A connection error or 5xx moves to the next one; health checks keep the pool current.',
  },
  {
    n: '05',
    k: 'route',
    title: 'Stream back',
    body: 'The upstream response is flushed to the caller as it arrives, never buffered, with X-Inferoute-Backend and X-Inferoute-Cache headers describing how it was handled.',
  },
]

const capabilities = [
  {
    k: 'route',
    title: 'Routing + failover',
    body: 'Round-robin across healthy backends per model, automatic retry on the next one on error.',
  },
  {
    k: 'route',
    title: 'Streaming passthrough',
    body: 'SSE responses are forwarded chunk by chunk as the backend produces them.',
  },
  {
    k: 'route',
    title: 'Per-key rate limiting',
    body: 'Token bucket on the API key or IP. In-process, or Redis-backed for a shared limit.',
  },
  {
    k: 'cache',
    title: 'Semantic cache',
    body: 'Prompts are embedded and matched in NuclaDB; close matches skip the backend entirely.',
  },
  {
    k: 'route',
    title: 'Config hot-reload',
    body: 'SIGHUP reloads backends, model aliases, keys, and limits without dropping in-flight requests.',
  },
  {
    k: 'cache',
    title: 'Prometheus metrics',
    body: 'Volume and latency by model and backend, cache hit / miss / error, on /metrics.',
  },
  {
    k: 'route',
    title: 'API-key allowlist',
    body: 'Optional: gate /v1/chat/completions behind a set of keys, constant-time compared.',
  },
  {
    k: 'cache',
    title: 'Model aliases',
    body: 'Map a friendly name (gpt-4o) onto whatever a backend actually serves (llama3:70b).',
  },
]

const backends = ['Ollama', 'vLLM', 'OpenAI', 'Anthropic', 'LM Studio', 'text-generation-inference', 'llama.cpp', 'Together']

const faq = [
  {
    q: 'Does my client code change?',
    a: 'Only the base URL. inferoute speaks the OpenAI chat-completions API, so any OpenAI SDK works by pointing it at the gateway.',
  },
  {
    q: 'What is the cache matching on?',
    a: 'Embedding distance between prompts, checked against NuclaDB. You set the max distance; anything closer is treated as a hit. Caching is off until you enable it.',
  },
  {
    q: 'How does it hold state?',
    a: 'It does not. inferouted is a single stateless binary reading one JSON config. Rate-limit and cache state can live in Redis / NuclaDB if you want them shared.',
  },
  {
    q: 'Is it multi-tenant?',
    a: 'The gateway itself is single-tenant. This dashboard adds per-account isolation: you register your own gateway connections and only ever see your own traffic.',
  },
]

function dot(k: string) {
  return k === 'cache' ? 'var(--cache)' : k === 'warn' ? 'var(--warn)' : 'var(--route)'
}

export default function Landing() {
  return (
    <div className="space-y-14 sm:space-y-20">
      <section className="pt-6 sm:pt-8">
        <Logo className="rise h-10 w-10 text-ink sm:h-11 sm:w-11" />
        <p className="eyebrow rise mt-5" style={{ '--d': '0.06s' } as React.CSSProperties}>
          OpenAI-compatible inference gateway
        </p>
        <h1
          className="rise mt-3 font-display text-[1.7rem] font-bold leading-[1.15] tracking-tight sm:text-5xl sm:leading-[1.1]"
          style={{ '--d': '0.12s' } as React.CSSProperties}
        >
          Route, cache, and rate-limit
          <br className="hidden sm:inline" />{' '}
          <span className="underline-draw relative inline-block text-route">your inference backends</span>
        </h1>
        <p
          className="rise mt-4 max-w-xl text-ink-dim"
          style={{ '--d': '0.18s' } as React.CSSProperties}
        >
          inferoute sits in front of your Ollama, vLLM, or hosted LLM endpoints and
          load-balances across them, health-checks them, fails over on error, and
          semantically caches responses. One binary, one JSON config.
        </p>
        <div
          className="rise mt-6 flex flex-wrap gap-3"
          style={{ '--d': '0.24s' } as React.CSSProperties}
        >
          <Link
            href="/dashboard"
            className="rounded bg-route px-4 py-2 font-display text-sm font-semibold text-void transition-transform hover:-translate-y-0.5"
          >
            Open dashboard
          </Link>
          <Link
            href="/docs"
            className="rounded border border-line px-4 py-2 font-display text-sm font-semibold text-ink transition-colors hover:border-ink-dim"
          >
            Read the docs
          </Link>
          <a
            href={GITHUB}
            target="_blank"
            rel="noreferrer"
            className="rounded border border-line px-4 py-2 font-display text-sm font-semibold text-ink-dim transition-colors hover:border-ink-dim hover:text-ink"
          >
            GitHub ↗
          </a>
        </div>
      </section>

      <Reveal>
        <DispatchSim />
        <p className="mt-2 text-xs text-ink-dim">
          Drive the load, toggle cache and the rate limit, knock backends offline. The
          pipeline responds the way the real gateway would.
        </p>
      </Reveal>

      <Reveal>
        <h2 className="eyebrow mb-6">what happens to a request</h2>
        <ol className="space-y-1">
          {flow.map((s, i) => (
            <li
              key={s.n}
              className="r-item group flex gap-4 rounded px-2 py-2 transition-colors hover:bg-panel/50"
              style={{ '--d': `${i * 0.06}s` } as React.CSSProperties}
            >
              <span className="mono-num shrink-0 pt-0.5 text-sm text-ink-dim">{s.n}</span>
              <div className="border-l border-line pl-4 transition-colors group-hover:border-ink-dim">
                <div className="flex items-center gap-2">
                  <span className="led" style={{ background: dot(s.k) }} />
                  <h3 className="font-display text-sm font-semibold">{s.title}</h3>
                </div>
                <p className="mt-1 max-w-2xl text-sm text-ink-dim">{s.body}</p>
              </div>
            </li>
          ))}
        </ol>
      </Reveal>

      <Reveal>
        <h2 className="eyebrow mb-4">what it does</h2>
        <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
          {capabilities.map((c, i) => (
            <div
              key={c.title}
              className="r-item panel p-4 transition-transform duration-200 hover:-translate-y-0.5 hover:border-ink-dim"
              style={{ '--d': `${0.03 * i}s` } as React.CSSProperties}
            >
              <div className="flex items-center gap-2">
                <span className="led" style={{ background: dot(c.k) }} />
                <h3 className="font-display text-sm font-semibold">{c.title}</h3>
              </div>
              <p className="mt-2 text-sm text-ink-dim">{c.body}</p>
            </div>
          ))}
        </div>
      </Reveal>

      <Reveal>
        <h2 className="eyebrow mb-4">put it in front of anything that speaks the API</h2>
        <div className="flex flex-wrap gap-2">
          {backends.map((b, i) => (
            <span
              key={b}
              className="r-item rounded border border-line bg-panel px-2.5 py-1 font-mono text-xs text-ink-dim transition-colors hover:border-ink-dim hover:text-ink"
              style={{ '--d': `${0.03 * i}s` } as React.CSSProperties}
            >
              {b}
            </span>
          ))}
        </div>
      </Reveal>

      <Reveal className="grid gap-6 lg:grid-cols-2">
        <div>
          <h2 className="eyebrow mb-4">config</h2>
          <pre className="panel overflow-x-auto p-4 font-mono text-xs leading-relaxed text-ink-dim">
{`{
  "listen_addr": ":8081",
  "backends": [
    { "name": "ollama-a", "url": "http://10.0.0.11:11434",
      "models": ["llama3", "mistral"] },
    { "name": "vllm-c", "url": "http://10.0.0.20:8000",
      "models": ["llama3:70b"] }
  ],
  "model_aliases": { "gpt-4o": "llama3:70b" },
  "rate_limit": { "enabled": true, "requests_per_second": 20, "burst": 40 },
  "cache": { "enabled": true, "max_distance": 0.15 },
  "api_keys": ["sk-team-1"]
}`}
          </pre>
        </div>
        <div>
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
            Then add it as a connection on the{' '}
            <Link href="/connections" className="text-ink hover:text-route">
              Connections
            </Link>{' '}
            page to watch it live here.
          </p>
        </div>
      </Reveal>

      <Reveal>
        <h2 className="eyebrow mb-4">questions</h2>
        <dl className="grid gap-4 sm:grid-cols-2">
          {faq.map((f, i) => (
            <div
              key={f.q}
              className="r-item panel p-4 transition-transform duration-200 hover:-translate-y-0.5 hover:border-ink-dim"
              style={{ '--d': `${0.04 * i}s` } as React.CSSProperties}
            >
              <dt className="font-display text-sm font-semibold text-ink">{f.q}</dt>
              <dd className="mt-1.5 text-sm text-ink-dim">{f.a}</dd>
            </div>
          ))}
        </dl>
      </Reveal>

      <footer className="border-t border-line pt-6 text-xs text-ink-dim">
        inferoute ·{' '}
        <a href={GITHUB} className="hover:text-ink">
          github.com/Rakshit-gen/inferoute
        </a>
      </footer>
    </div>
  )
}
