'use client'

import { useMemo, useRef, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { fetchBackends, fetchConfig, sendChat, ChatResult } from '@/lib/api'
import { DispatchTrace } from '@/components/dispatch-strip'
import { ms } from '@/lib/format'

function prettyBody(body: string): string {
  try {
    return JSON.stringify(JSON.parse(body), null, 2)
  } catch {
    return body
  }
}

function extractContent(body: string): string | null {
  try {
    const j = JSON.parse(body)
    return j.message?.content ?? j.choices?.[0]?.message?.content ?? null
  } catch {
    return null
  }
}

export default function Playground() {
  const { data: backends } = useQuery({ queryKey: ['backends'], queryFn: fetchBackends })
  const { data: config } = useQuery({ queryKey: ['config'], queryFn: fetchConfig })

  const models = useMemo(() => {
    const s = new Set<string>()
    for (const b of backends ?? []) for (const m of b.models ?? []) s.add(m)
    for (const a of Object.keys(config?.model_aliases ?? {})) s.add(a)
    return [...s]
  }, [backends, config])

  const [model, setModel] = useState('')
  const [prompt, setPrompt] = useState('In one sentence, what is a reverse proxy?')
  const [apiKey, setApiKey] = useState('')
  const [result, setResult] = useState<ChatResult | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [pending, setPending] = useState(false)
  const abortRef = useRef<AbortController | null>(null)

  const effectiveModel = model || models[0] || ''

  async function run() {
    if (!effectiveModel || !prompt.trim() || pending) return
    setPending(true)
    setError(null)
    abortRef.current?.abort()
    const ctrl = new AbortController()
    abortRef.current = ctrl
    try {
      setResult(await sendChat(effectiveModel, prompt, apiKey, ctrl.signal))
    } catch (e) {
      setError(e instanceof Error ? e.message : 'request failed')
    } finally {
      setPending(false)
    }
  }

  const content = result ? extractContent(result.body) : null

  return (
    <div className="space-y-6">
      <div>
        <h1 className="font-display text-2xl font-semibold tracking-tight">Playground</h1>
        <p className="text-sm text-ink-dim">
          Send one completion through the gateway and see exactly how it was routed.
        </p>
      </div>

      <div className="grid gap-6 lg:grid-cols-[minmax(0,1fr)_minmax(0,1.1fr)]">
        <section className="panel space-y-4 p-4">
          <div>
            <label className="eyebrow mb-1 block">model</label>
            <select
              value={effectiveModel}
              onChange={(e) => setModel(e.target.value)}
              className="w-full rounded border border-line bg-panel-2 px-2 py-1.5 font-mono text-sm outline-none focus:border-route"
            >
              {models.length === 0 && <option value="">no models available</option>}
              {models.map((m) => (
                <option key={m} value={m}>
                  {m}
                  {config?.model_aliases?.[m] ? ` → ${config.model_aliases[m]}` : ''}
                </option>
              ))}
            </select>
          </div>

          <div>
            <label className="eyebrow mb-1 block">prompt</label>
            <textarea
              value={prompt}
              onChange={(e) => setPrompt(e.target.value)}
              rows={5}
              className="w-full resize-y rounded border border-line bg-panel-2 px-2 py-1.5 text-sm outline-none focus:border-route"
            />
          </div>

          <div>
            <label className="eyebrow mb-1 block">api key · optional</label>
            <input
              value={apiKey}
              onChange={(e) => setApiKey(e.target.value)}
              type="password"
              placeholder="sent as Authorization: Bearer …"
              className="w-full rounded border border-line bg-panel-2 px-2 py-1.5 font-mono text-sm outline-none focus:border-route"
            />
            <p className="mt-1 text-[0.65rem] text-ink-dim">
              Held in this tab only — used for rate-limit bucketing and gated backends.
            </p>
          </div>

          <button
            onClick={run}
            disabled={pending || !effectiveModel || !prompt.trim()}
            className="w-full rounded bg-route px-3 py-2 font-display text-sm font-semibold text-void transition-opacity disabled:opacity-40"
          >
            {pending ? 'dispatching…' : 'Send'}
          </button>
        </section>

        <section className="space-y-4">
          {error && (
            <div className="panel border-alert/40 p-4 text-sm text-alert">
              {error}
              <p className="mt-1 text-xs text-ink-dim">
                If this is a CORS or network error, confirm the gateway URL and that its
                <span className="font-mono"> cors_origins</span> allows this page.
              </p>
            </div>
          )}

          {result && (
            <>
              <div className="panel p-4">
                <div className="mb-3 flex items-center gap-2">
                  <span className="eyebrow">route taken</span>
                  <span className="mono-num ml-auto text-xs text-ink-dim">{ms(result.latencyMs)}</span>
                  {result.cacheHit && (
                    <span className="rounded bg-cache/20 px-1.5 py-0.5 font-mono text-[0.65rem] text-cache">
                      cache hit
                    </span>
                  )}
                </div>
                <DispatchTrace
                  servedBy={result.servedBy}
                  cacheHit={result.cacheHit}
                  latencyMs={result.latencyMs}
                  status={result.status}
                />
              </div>

              {content && (
                <div className="panel p-4">
                  <div className="eyebrow mb-2">completion</div>
                  <p className="whitespace-pre-wrap text-sm text-ink">{content}</p>
                </div>
              )}

              <details className="panel p-4">
                <summary className="eyebrow cursor-pointer">raw response · HTTP {result.status}</summary>
                <pre className="mono-num mt-2 max-h-80 overflow-auto text-xs text-ink-dim">
                  {prettyBody(result.body)}
                </pre>
              </details>
            </>
          )}

          {!result && !error && (
            <div className="panel p-8 text-center text-sm text-ink-dim">
              Send a request to see the dispatch trace.
            </div>
          )}
        </section>
      </div>
    </div>
  )
}
