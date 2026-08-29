'use client'

import { useQuery } from '@tanstack/react-query'
import { fetchBackends } from '@/lib/api'
import { MetricsView } from '@/lib/metrics'
import { num } from '@/lib/format'

export function BackendBoard({ metrics }: { metrics?: MetricsView }) {
  const { data: backends, error, isLoading } = useQuery({
    queryKey: ['backends'],
    queryFn: fetchBackends,
    refetchInterval: 5000,
  })

  const countFor = (name: string) =>
    metrics?.byBackend.find((b) => b.backend === name)?.count ?? 0

  return (
    <section className="panel p-4">
      <div className="eyebrow mb-3">backends</div>
      {isLoading && <p className="text-sm text-ink-dim">connecting…</p>}
      {error && (
        <p className="text-sm text-alert">
          can’t reach the gateway: check the active connection on the Connections page
        </p>
      )}
      <ul className="divide-y divide-line">
        {backends?.map((b) => (
          <li key={b.name} className="flex items-center gap-3 py-2.5">
            <span
              className={`led animate-led-pulse ${b.healthy ? 'bg-route' : 'bg-alert'}`}
              style={{ boxShadow: `0 0 8px ${b.healthy ? 'var(--route)' : 'var(--alert)'}` }}
            />
            <div className="min-w-0 flex-1">
              <div className="flex items-baseline gap-2">
                <span className="font-mono text-sm text-ink">{b.name}</span>
                <span className="mono-num truncate text-xs text-ink-dim">{b.url}</span>
              </div>
              <div className="mt-0.5 flex flex-wrap gap-1">
                {(b.models ?? []).map((m) => (
                  <span key={m} className="rounded bg-panel-2 px-1.5 py-0.5 font-mono text-[0.65rem] text-ink-dim">
                    {m}
                  </span>
                ))}
              </div>
            </div>
            <div className="text-right">
              <div className="mono-num text-sm text-ink">{num(countFor(b.name))}</div>
              <div className="text-[0.65rem] text-ink-dim">served</div>
            </div>
          </li>
        ))}
      </ul>
      {backends?.length === 0 && <p className="text-sm text-ink-dim">no backends configured</p>}
    </section>
  )
}
