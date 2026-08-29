'use client'

import { useMemo, useRef, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { fetchBackends, fetchConfig, sendChat, ChatResult, NoGatewayError } from '@/lib/api'
import { useConnections } from '@/lib/use-connections'
import { DispatchTrace } from '@/components/dispatch-strip'
import { EmptyGateway } from '@/components/empty-gateway'
import { ms } from '@/lib/format'

function prettyBody(body: string): string {
  try {
    return JSON.stringify(JSON.parse(body), null, 2)
  } catch {
    return body
  }
}

// A human sentence for the status the gateway (or this app) returned.
function explain(status: number): string | null {
  if (status >= 200 && status < 300) return null
  switch (status) {
    case 401:
      return 'The gateway rejected the API key. Check the key on the Connections page.'
    case 403:
      return 'The gateway refused this request (403).'
    case 409:
      return 'No gateway connection is active. Add or select one on the Connections page.'
    case 429:
      return 'Rate limited by the gateway. Wait a moment and try again.'
    case 502:
      return 'Every backend the gateway tried failed (502). Check the backends are up.'
    case 503:
      return 'No healthy backend serves that model right now (503).'
    default:
      return `The gateway returned HTTP ${status}.`
  }
}

export default function Playground() {
  const { active, isLoading: connLoading } = useConnections()
  const ready = Boolean(active)
  const { data: backends } = useQuery({ queryKey: ['backends'], queryFn: fetchBackends, enabled: ready, retry: false })
  const { data: config } = useQuery({ queryKey: ['config'], queryFn: fetchConfig, enabled: ready, retry: false })

  const models = useMemo(() => {
    const s = new Set<string>()
    for (const b of backends ?? []) for (const m of b.models ?? []) s.add(m)
    for (const a of Object.keys(config?.model_aliases ?? {})) s.add(a)
    return [...s]
  }, [backends, config])

  const [model, setModel] = useState('')
  const [prompt, setPrompt] = useState('In one sentence, what is a reverse proxy?')
  const [stream, setStream] = useState(true)
  const [result, setResult] = useState<ChatResult | null>(null)
  const [live, setLive] = useState('')
  const [error, setError] = useState<string | null>(null)
  const [pending, setPending] = useState(false)
  const abortRef = useRef<AbortController | null>(null)

  const effectiveModel = model || models[0] || ''

  async function run() {
    if (!effectiveModel || !prompt.trim() || pending) return
    setPending(true)
    setError(null)
    setResult(null)
    setLive('')
    abortRef.current?.abort()
    const ctrl = new AbortController()
    abortRef.current = ctrl
    try {
      const r = await sendChat(effectiveModel, prompt, {
        stream,
        signal: ctrl.signal,
        onProgress: setLive,
      })
      setResult(r)
      const why = explain(r.status)
      if (why) setError(why + (r.body ? ` · ${r.body.trim().slice(0, 200)}` : ''))
    } catch (e) {
      if (e instanceof NoGatewayError) setError(explain(409)!)
      else if (e instanceof DOMException && e.name === 'AbortError') {
        /* superseded by a newer request */
      } else setError(e instanceof Error ? e.message : 'request failed')
    } finally {
      setPending(false)
    }
  }

  const shownContent = result?.content ?? (live || null)

  if (!connLoading && !active) {
    return (
      <div className="space-y-6">
        <h1 className="font-display text-2xl font-semibold tracking-tight">Playground</h1>
        <EmptyGateway />
      </div>
    )
  }

  return (
    <div className="space-y-6">
      <div>
        <h1 className="font-display text-2xl font-semibold tracking-tight">Playground</h1>
        <p className="text-sm text-ink-dim">
          Send one completion through your active gateway and see exactly how it was routed.
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
                  {config?.model_aliases?.[m] ? ` -> ${config.model_aliases[m]}` : ''}
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

          <label className="flex items-center gap-2 text-sm text-ink">
            <input
              type="checkbox"
              checked={stream}
              onChange={(e) => setStream(e.target.checked)}
              className="accent-route"
            />
            stream the response
          </label>

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
                Connection settings live on the{' '}
                <span className="font-mono">Connections</span> page.
              </p>
            </div>
          )}

          {(shownContent || pending) && (
            <div className="panel p-4">
              <div className="eyebrow mb-2 flex items-center gap-2">
                completion
                {pending && result === null && (
                  <span className="text-[0.65rem] text-ink-dim">streaming…</span>
                )}
              </div>
              <p className="min-h-[1.5rem] whitespace-pre-wrap text-sm text-ink">
                {shownContent}
                {pending && result === null && <span className="animate-pulse">▍</span>}
              </p>
            </div>
          )}

          {result && (
            <>
              <div className="panel p-4">
                <div className="mb-3 flex flex-wrap items-center gap-2">
                  <span className="eyebrow">route taken</span>
                  <span className="mono-num ml-auto text-xs text-ink-dim">
                    {result.ttfbMs != null && `first byte ${ms(result.ttfbMs)} · `}
                    {ms(result.latencyMs)}
                  </span>
                  {result.streamed && (
                    <span className="rounded bg-panel-2 px-1.5 py-0.5 font-mono text-[0.65rem] text-ink-dim">
                      streamed
                    </span>
                  )}
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

              <details className="panel p-4">
                <summary className="eyebrow cursor-pointer">raw response · HTTP {result.status}</summary>
                <pre className="mono-num mt-2 max-h-80 overflow-auto text-xs text-ink-dim">
                  {prettyBody(result.body)}
                </pre>
              </details>
            </>
          )}

          {!result && !error && !pending && (
            <div className="panel p-8 text-center text-sm text-ink-dim">
              Send a request to see the dispatch trace.
            </div>
          )}
        </section>
      </div>
    </div>
  )
}
