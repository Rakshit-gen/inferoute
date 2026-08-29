'use client'

import { useState } from 'react'
import { useConnections, type PublicConnection, type ProbeResult } from '@/lib/use-connections'
import { ms } from '@/lib/format'

const DEFAULT_URL = process.env.NEXT_PUBLIC_DEFAULT_GATEWAY_URL || ''

const inputCls =
  'w-full rounded border border-line bg-panel-2 px-2 py-1.5 text-sm outline-none focus:border-route'

export default function ConnectionsPage() {
  const conns = useConnections()
  const { connections, activeConnectionId, isLoading, add } = conns
  const [name, setName] = useState('')
  const [url, setUrl] = useState(DEFAULT_URL)
  const [apiKey, setApiKey] = useState('')

  const submit = (e: React.FormEvent) => {
    e.preventDefault()
    if (!url.trim()) return
    add.mutate(
      { name, url, apiKey: apiKey || undefined },
      {
        onSuccess: () => {
          setName('')
          setApiKey('')
        },
      },
    )
  }

  return (
    <div className="space-y-6">
      <div>
        <h1 className="font-display text-2xl font-semibold tracking-tight">Connections</h1>
        <p className="text-sm text-ink-dim">
          Each gateway you register here is private to your account. The dashboard and playground
          read from whichever one is active.
        </p>
      </div>

      <form onSubmit={submit} className="panel space-y-3 p-4">
        <div className="eyebrow">add a gateway</div>
        <div className="grid gap-3 sm:grid-cols-2">
          <label className="block">
            <span className="eyebrow mb-1 block">name · optional</span>
            <input value={name} onChange={(e) => setName(e.target.value)} placeholder="prod-us-east" className={inputCls} />
          </label>
          <label className="block">
            <span className="eyebrow mb-1 block">gateway url</span>
            <input
              value={url}
              onChange={(e) => setUrl(e.target.value)}
              required
              placeholder="https://gateway.example.com"
              className={`${inputCls} font-mono`}
            />
          </label>
        </div>
        <label className="block">
          <span className="eyebrow mb-1 block">api key · optional</span>
          <input
            value={apiKey}
            onChange={(e) => setApiKey(e.target.value)}
            type="password"
            placeholder="stored server-side, sent as Authorization: Bearer"
            className={`${inputCls} font-mono`}
          />
        </label>
        {add.error && <p className="text-sm text-alert">{(add.error as Error).message}</p>}
        <button
          type="submit"
          disabled={add.isPending || !url.trim()}
          className="rounded bg-route px-3 py-2 font-display text-sm font-semibold text-void disabled:opacity-40"
        >
          {add.isPending ? 'adding…' : 'Add connection'}
        </button>
      </form>

      <section className="panel divide-y divide-line">
        {isLoading && <p className="p-4 text-sm text-ink-dim">loading…</p>}
        {!isLoading && connections.length === 0 && (
          <p className="p-4 text-sm text-ink-dim">No connections yet.</p>
        )}
        {connections.map((c) => (
          <ConnectionRow
            key={c.id}
            conn={c}
            isActive={c.id === activeConnectionId}
            hooks={conns}
          />
        ))}
      </section>
    </div>
  )
}

function ConnectionRow({
  conn,
  isActive,
  hooks,
}: {
  conn: PublicConnection
  isActive: boolean
  hooks: ReturnType<typeof useConnections>
}) {
  const { update, remove, setActive, test } = hooks
  const [editing, setEditing] = useState(false)
  const [name, setName] = useState(conn.name)
  const [url, setUrl] = useState(conn.url)
  const [apiKey, setApiKey] = useState('')
  const [probe, setProbe] = useState<ProbeResult | null>(null)

  const runTest = () => {
    setProbe(null)
    test.mutate(conn.id, { onSuccess: setProbe })
  }

  const save = () => {
    update.mutate(
      { id: conn.id, name, url, apiKey: apiKey ? apiKey : undefined },
      { onSuccess: () => setEditing(false) },
    )
  }

  return (
    <div className="p-4">
      <div className="flex flex-wrap items-center gap-3">
        <span className={`led ${isActive ? 'bg-route' : 'bg-ink-dim'}`} />
        <div className="min-w-0 flex-1">
          <div className="flex items-baseline gap-2">
            <span className="text-sm text-ink">{conn.name}</span>
            {isActive && <span className="eyebrow text-route">active</span>}
          </div>
          <div className="mono-num truncate text-xs text-ink-dim">
            {conn.url}
            {conn.hasApiKey ? ' · keyed' : ''}
          </div>
        </div>
        <button
          onClick={runTest}
          disabled={test.isPending}
          className="rounded border border-line px-2.5 py-1 text-xs text-ink-dim hover:text-ink"
        >
          {test.isPending ? 'testing…' : 'Test'}
        </button>
        {!isActive && (
          <button
            onClick={() => setActive.mutate(conn.id)}
            disabled={setActive.isPending}
            className="rounded border border-line px-2.5 py-1 text-xs text-ink-dim hover:text-ink"
          >
            Set active
          </button>
        )}
        <button
          onClick={() => setEditing((v) => !v)}
          className="rounded border border-line px-2.5 py-1 text-xs text-ink-dim hover:text-ink"
        >
          {editing ? 'Cancel' : 'Edit'}
        </button>
        <button
          onClick={() => remove.mutate(conn.id)}
          disabled={remove.isPending}
          className="rounded border border-alert/40 px-2.5 py-1 text-xs text-alert hover:bg-alert/10"
        >
          Remove
        </button>
      </div>

      {probe && <ProbeLine probe={probe} />}
      {test.error && <p className="mt-2 text-xs text-alert">{test.error.message}</p>}

      {editing && (
        <div className="mt-3 grid gap-2 border-t border-line pt-3 sm:grid-cols-3">
          <input value={name} onChange={(e) => setName(e.target.value)} placeholder="name" className={inputCls} />
          <input
            value={url}
            onChange={(e) => setUrl(e.target.value)}
            placeholder="gateway url"
            className={`${inputCls} font-mono sm:col-span-2`}
          />
          <input
            value={apiKey}
            onChange={(e) => setApiKey(e.target.value)}
            type="password"
            placeholder={conn.hasApiKey ? 'api key (leave blank to keep current)' : 'api key (optional)'}
            className={`${inputCls} font-mono sm:col-span-2`}
          />
          <div className="flex gap-2">
            <button
              onClick={save}
              disabled={update.isPending}
              className="rounded bg-route px-3 py-1.5 font-display text-xs font-semibold text-void disabled:opacity-40"
            >
              Save
            </button>
            {conn.hasApiKey && (
              <button
                onClick={() => update.mutate({ id: conn.id, apiKey: null })}
                disabled={update.isPending}
                className="rounded border border-line px-3 py-1.5 text-xs text-ink-dim hover:text-ink"
              >
                Clear key
              </button>
            )}
          </div>
          {update.error && <p className="text-xs text-alert sm:col-span-3">{(update.error as Error).message}</p>}
        </div>
      )}
    </div>
  )
}

function ProbeLine({ probe }: { probe: ProbeResult }) {
  if (!probe.reachable) {
    return <p className="mt-2 text-xs text-alert">unreachable: {probe.error}</p>
  }
  if (probe.authOk === false) {
    return (
      <p className="mt-2 text-xs text-warn">
        reachable, but the gateway rejected the API key (HTTP {probe.status})
      </p>
    )
  }
  const feats: string[] = []
  if (probe.config?.rate_limit?.enabled) feats.push('rate limit on')
  if (probe.config?.cache?.enabled) feats.push('cache on')
  if (probe.config?.api_keys_required) feats.push('keys required')
  return (
    <p className="mt-2 text-xs text-route">
      reachable{probe.latencyMs != null ? ` in ${ms(probe.latencyMs)}` : ''}
      {feats.length ? ` · ${feats.join(' · ')}` : ''}
    </p>
  )
}
