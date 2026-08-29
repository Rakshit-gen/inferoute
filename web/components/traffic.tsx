'use client'

import { MetricsView } from '@/lib/metrics'
import { ms, num, pct, statusColor } from '@/lib/format'
import { Sparkline } from './sparkline'

function Tile({
  label,
  value,
  hint,
  tone,
}: {
  label: string
  value: string
  hint?: string
  tone?: string
}) {
  return (
    <div className="panel p-3">
      <div className="eyebrow">{label}</div>
      <div className="mono-num mt-1 text-2xl" style={{ color: tone }}>
        {value}
      </div>
      {hint && <div className="mono-num text-[0.65rem] text-ink-dim">{hint}</div>}
    </div>
  )
}

export function Traffic({
  view,
  rateHistory,
  latencyHistory,
}: {
  view?: MetricsView
  rateHistory: number[]
  latencyHistory: number[]
}) {
  const maxStatus = Math.max(1, ...(view?.byStatus.map((s) => s.count) ?? [1]))

  return (
    <section className="space-y-3">
      <div className="grid grid-cols-2 gap-3 md:grid-cols-4">
        <Tile label="requests" value={view ? num(view.totalRequests) : '—'} hint="since start" />
        <Tile
          label="error rate"
          value={view ? pct(view.errorRate) : '—'}
          tone={view && view.errorRate > 0.05 ? 'var(--alert)' : undefined}
          hint="4xx + 5xx"
        />
        <Tile
          label="p99 latency"
          value={view?.latency ? ms(view.latency.p99) : '—'}
          hint={view?.latency ? `p50 ${ms(view.latency.p50)}` : 'no samples'}
        />
        <Tile
          label="cache hit rate"
          value={view ? pct(view.cache.hitRate) : '—'}
          tone="var(--cache)"
          hint={view ? `${num(view.cache.hit)} hit / ${num(view.cache.miss)} miss` : undefined}
        />
      </div>

      <div className="grid gap-3 md:grid-cols-2">
        <div className="panel p-4">
          <div className="eyebrow mb-2">throughput · req/s</div>
          <Sparkline points={rateHistory} />
        </div>
        <div className="panel p-4">
          <div className="eyebrow mb-2">p99 latency · ms</div>
          <Sparkline points={latencyHistory} color="var(--cache)" />
        </div>
      </div>

      <div className="panel p-4">
        <div className="eyebrow mb-3">status codes</div>
        {(!view || view.byStatus.length === 0) && (
          <p className="text-sm text-ink-dim">no requests yet</p>
        )}
        <ul className="space-y-1.5">
          {view?.byStatus.map((s) => (
            <li key={s.status} className="flex items-center gap-3">
              <span className="mono-num w-10 text-sm" style={{ color: statusColor(s.status) }}>
                {s.status}
              </span>
              <div className="h-2 flex-1 overflow-hidden rounded bg-panel-2">
                <div
                  className="h-full rounded"
                  style={{ width: `${(s.count / maxStatus) * 100}%`, background: statusColor(s.status) }}
                />
              </div>
              <span className="mono-num w-14 text-right text-sm text-ink-dim">{num(s.count)}</span>
            </li>
          ))}
        </ul>
      </div>
    </section>
  )
}
