'use client'

import { ms } from '@/lib/format'

function Node({
  label,
  sub,
  tone = 'idle',
}: {
  label: string
  sub?: string
  tone?: 'idle' | 'route' | 'cache' | 'dim'
}) {
  const ring =
    tone === 'route'
      ? 'border-route text-route'
      : tone === 'cache'
        ? 'border-cache text-cache'
        : tone === 'dim'
          ? 'border-line text-ink-dim'
          : 'border-line text-ink'
  return (
    <div className={`shrink-0 rounded border ${ring} bg-panel px-3 py-1.5 text-center`}>
      <div className="font-mono text-xs">{label}</div>
      {sub && <div className="mono-num text-[0.65rem] text-ink-dim">{sub}</div>}
    </div>
  )
}

function Wire({ active }: { active?: boolean }) {
  return (
    <div className="relative mx-1 h-px min-w-[24px] flex-1 self-center">
      <div className={`absolute inset-0 ${active ? 'bg-route' : 'wire'}`} />
      <div
        className={`absolute right-0 top-1/2 -mt-1 h-0 w-0 -translate-y-px border-y-4 border-l-[6px] border-y-transparent ${
          active ? 'border-l-route' : 'border-l-line'
        }`}
      />
    </div>
  )
}

/** Animated synthetic traffic — the page's signature element. */
export function DispatchStrip({
  rateLimit,
  cache,
  backends,
}: {
  rateLimit: boolean
  cache: boolean
  backends: string[]
}) {
  const dest = backends[0] ?? 'origin pool'
  return (
    <div className="panel overflow-hidden p-4">
      <div className="eyebrow mb-3">dispatch path</div>
      <div className="relative flex items-stretch overflow-x-auto pb-1">
        {/* travelling pip */}
        <div className="pointer-events-none absolute inset-x-4 top-1/2">
          <span className="dispatch-pip animate-pip" />
        </div>
        <Node label="ingress" sub="POST /v1/chat" />
        <Wire />
        <Node label="rate-limit" sub={rateLimit ? 'token bucket' : 'off'} tone={rateLimit ? 'idle' : 'dim'} />
        <Wire />
        <Node label="cache" sub={cache ? 'semantic · NuclaDB' : 'off'} tone={cache ? 'cache' : 'dim'} />
        <Wire />
        <Node label="round-robin" sub={`${backends.length || '—'} backend${backends.length === 1 ? '' : 's'}`} />
        <Wire active />
        <Node label={dest} sub="healthy" tone="route" />
      </div>
    </div>
  )
}

/** Static trace of one real request that just ran through the gateway. */
export function DispatchTrace({
  servedBy,
  cacheHit,
  latencyMs,
  status,
}: {
  servedBy: string | null
  cacheHit: boolean
  latencyMs: number
  status: number
}) {
  const ok = status >= 200 && status < 300
  return (
    <div className="flex flex-wrap items-stretch gap-y-2">
      <Node label="ingress" sub={`HTTP ${status}`} tone={ok ? 'idle' : 'dim'} />
      <Wire active={ok} />
      {cacheHit ? (
        <>
          <Node label="cache" sub="hit" tone="cache" />
          <Wire active />
          <Node label="replayed" sub={ms(latencyMs)} tone="cache" />
        </>
      ) : (
        <>
          <Node label="cache" sub="miss" tone="dim" />
          <Wire active={ok} />
          <Node label="round-robin" sub="picked next healthy" tone={ok ? 'idle' : 'dim'} />
          <Wire active={ok} />
          <Node label={servedBy ?? '—'} sub={ms(latencyMs)} tone={ok ? 'route' : 'dim'} />
        </>
      )}
    </div>
  )
}
