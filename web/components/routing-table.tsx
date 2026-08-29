'use client'

import { useQuery } from '@tanstack/react-query'
import { fetchBackends, fetchConfig } from '@/lib/api'

export function RoutingTable() {
  const { data: backends } = useQuery({ queryKey: ['backends'], queryFn: fetchBackends, refetchInterval: 5000 })
  const { data: config } = useQuery({ queryKey: ['config'], queryFn: fetchConfig, refetchInterval: 15000 })

  // model -> backends that serve it
  const byModel = new Map<string, { name: string; healthy: boolean }[]>()
  for (const b of backends ?? []) {
    for (const m of b.models ?? []) {
      const list = byModel.get(m) ?? []
      list.push({ name: b.name, healthy: b.healthy })
      byModel.set(m, list)
    }
  }
  const aliases = Object.entries(config?.model_aliases ?? {})

  return (
    <section className="panel p-4">
      <div className="eyebrow mb-3">routing table</div>

      {aliases.length > 0 && (
        <div className="mb-4 space-y-1">
          {aliases.map(([from, to]) => (
            <div key={from} className="flex items-center gap-2 font-mono text-xs">
              <span className="rounded bg-panel-2 px-1.5 py-0.5 text-cache">{from}</span>
              <span className="text-ink-dim">alias →</span>
              <span className="rounded bg-panel-2 px-1.5 py-0.5 text-ink">{to}</span>
            </div>
          ))}
        </div>
      )}

      <ul className="space-y-2">
        {[...byModel.entries()].map(([model, pool]) => (
          <li key={model} className="flex flex-wrap items-center gap-2 border-t border-line pt-2 first:border-0 first:pt-0">
            <span className="font-mono text-sm text-ink">{model}</span>
            <span className="text-ink-dim">══▶</span>
            {pool.map((p) => (
              <span
                key={p.name}
                className={`flex items-center gap-1.5 rounded border px-2 py-0.5 font-mono text-xs ${
                  p.healthy ? 'border-line text-ink' : 'border-alert/40 text-ink-dim line-through'
                }`}
              >
                <span className={`led ${p.healthy ? 'bg-route' : 'bg-alert'}`} />
                {p.name}
              </span>
            ))}
          </li>
        ))}
      </ul>
      {byModel.size === 0 && <p className="text-sm text-ink-dim">no models routed</p>}
    </section>
  )
}
