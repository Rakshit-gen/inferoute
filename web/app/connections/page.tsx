'use client'

import { useState } from 'react'
import { useConnections } from '@/lib/use-connections'

const DEFAULT_URL = process.env.NEXT_PUBLIC_DEFAULT_GATEWAY_URL || ''

export default function ConnectionsPage() {
  const { connections, activeConnectionId, isLoading, add, remove, setActive } = useConnections()
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
          Each gateway you register here is private to your account. The dashboard and
          playground read from whichever one is active.
        </p>
      </div>

      <form onSubmit={submit} className="panel space-y-3 p-4">
        <div className="eyebrow">add a gateway</div>
        <div className="grid gap-3 sm:grid-cols-2">
          <label className="block">
            <span className="eyebrow mb-1 block">name · optional</span>
            <input
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="prod-us-east"
              className="w-full rounded border border-line bg-panel-2 px-2 py-1.5 text-sm outline-none focus:border-route"
            />
          </label>
          <label className="block">
            <span className="eyebrow mb-1 block">gateway url</span>
            <input
              value={url}
              onChange={(e) => setUrl(e.target.value)}
              required
              placeholder="https://gateway.example.com"
              className="w-full rounded border border-line bg-panel-2 px-2 py-1.5 font-mono text-sm outline-none focus:border-route"
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
            className="w-full rounded border border-line bg-panel-2 px-2 py-1.5 font-mono text-sm outline-none focus:border-route"
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
        {connections.map((c) => {
          const isActive = c.id === activeConnectionId
          return (
            <div key={c.id} className="flex flex-wrap items-center gap-3 p-4">
              <span className={`led ${isActive ? 'bg-route' : 'bg-ink-dim'}`} />
              <div className="min-w-0 flex-1">
                <div className="flex items-baseline gap-2">
                  <span className="text-sm text-ink">{c.name}</span>
                  {isActive && <span className="eyebrow text-route">active</span>}
                </div>
                <div className="mono-num truncate text-xs text-ink-dim">
                  {c.url}
                  {c.hasApiKey ? ' · keyed' : ''}
                </div>
              </div>
              {!isActive && (
                <button
                  onClick={() => setActive.mutate(c.id)}
                  disabled={setActive.isPending}
                  className="rounded border border-line px-2.5 py-1 text-xs text-ink-dim hover:text-ink"
                >
                  Set active
                </button>
              )}
              <button
                onClick={() => remove.mutate(c.id)}
                disabled={remove.isPending}
                className="rounded border border-alert/40 px-2.5 py-1 text-xs text-alert hover:bg-alert/10"
              >
                Remove
              </button>
            </div>
          )
        })}
      </section>
    </div>
  )
}
