'use client'

import { useQuery } from '@tanstack/react-query'
import { fetchConfig } from '@/lib/api'
import { MetricsView } from '@/lib/metrics'
import { num, pct } from '@/lib/format'

export function CachePanel({ metrics }: { metrics?: MetricsView }) {
  const { data: config } = useQuery({ queryKey: ['config'], queryFn: fetchConfig, refetchInterval: 15000 })

  if (config && !config.cache.enabled) {
    return (
      <section className="panel p-4">
        <div className="eyebrow mb-2">semantic cache</div>
        <p className="text-sm text-ink-dim">
          disabled. Set <span className="font-mono text-cache">cache.enabled</span> in the config
          (needs a running NuclaDB) to embed prompts and serve close matches from cache.
        </p>
      </section>
    )
  }

  const c = metrics?.cache
  const total = c ? c.hit + c.miss + c.error : 0

  return (
    <section className="panel p-4">
      <div className="flex items-baseline justify-between">
        <div className="eyebrow">semantic cache</div>
        {config && (
          <span className="mono-num text-[0.65rem] text-ink-dim">
            max distance {config.cache.max_distance}
          </span>
        )}
      </div>

      <div className="mt-3 flex h-2.5 overflow-hidden rounded bg-panel-2">
        <div className="bg-cache" style={{ width: `${total ? (c!.hit / total) * 100 : 0}%` }} />
        <div className="bg-ink-dim/40" style={{ width: `${total ? (c!.miss / total) * 100 : 0}%` }} />
        <div className="bg-alert" style={{ width: `${total ? (c!.error / total) * 100 : 0}%` }} />
      </div>

      <dl className="mt-3 grid grid-cols-3 gap-2 text-center">
        <div>
          <dt className="text-[0.65rem] text-ink-dim">hit</dt>
          <dd className="mono-num text-lg text-cache">{c ? num(c.hit) : '—'}</dd>
        </div>
        <div>
          <dt className="text-[0.65rem] text-ink-dim">miss</dt>
          <dd className="mono-num text-lg text-ink">{c ? num(c.miss) : '—'}</dd>
        </div>
        <div>
          <dt className="text-[0.65rem] text-ink-dim">error</dt>
          <dd className="mono-num text-lg text-alert">{c ? num(c.error) : '—'}</dd>
        </div>
      </dl>
      <p className="mono-num mt-2 text-center text-xs text-ink-dim">
        {c ? pct(c.hitRate) : '—'} hit rate
      </p>
    </section>
  )
}
