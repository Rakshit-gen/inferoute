'use client'

import { useQuery } from '@tanstack/react-query'
import { fetchBackends, fetchConfig } from '@/lib/api'
import { useMetrics } from '@/lib/use-metrics'
import { DispatchStrip } from '@/components/dispatch-strip'
import { BackendBoard } from '@/components/backend-board'
import { Traffic } from '@/components/traffic'
import { RoutingTable } from '@/components/routing-table'
import { CachePanel } from '@/components/cache-panel'

export default function Dashboard() {
  const { view, rateHistory, latencyHistory } = useMetrics()
  const { data: backends } = useQuery({ queryKey: ['backends'], queryFn: fetchBackends, refetchInterval: 5000 })
  const { data: config } = useQuery({ queryKey: ['config'], queryFn: fetchConfig, refetchInterval: 15000 })

  return (
    <div className="space-y-6">
      <div>
        <h1 className="font-display text-2xl font-semibold tracking-tight">Dispatch</h1>
        <p className="text-sm text-ink-dim">
          Every request that hits inferoute takes this path. Live from the gateway.
        </p>
      </div>

      <DispatchStrip
        rateLimit={config?.rate_limit.enabled ?? false}
        cache={config?.cache.enabled ?? false}
        backends={(backends ?? []).map((b) => b.name)}
      />

      <Traffic view={view} rateHistory={rateHistory} latencyHistory={latencyHistory} />

      <div className="grid gap-6 lg:grid-cols-2">
        <BackendBoard metrics={view} />
        <div className="space-y-6">
          <RoutingTable />
          <CachePanel metrics={view} />
        </div>
      </div>
    </div>
  )
}
