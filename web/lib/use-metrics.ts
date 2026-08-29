'use client'

import { useQuery } from '@tanstack/react-query'
import { useEffect, useRef, useState } from 'react'
import { fetchMetricsText } from './api'
import { parsePrometheus, summarize, MetricsView } from './metrics'

const MAX_POINTS = 40

/** Polls /metrics, parses it, and keeps short req/s and p99 histories. */
export function useMetrics(intervalMs = 4000) {
  const { data, error, isLoading } = useQuery({
    queryKey: ['metrics'],
    queryFn: fetchMetricsText,
    refetchInterval: intervalMs,
  })

  const [view, setView] = useState<MetricsView | undefined>()
  const [rateHistory, setRateHistory] = useState<number[]>([])
  const [latencyHistory, setLatencyHistory] = useState<number[]>([])
  const prev = useRef<{ t: number; total: number } | null>(null)

  useEffect(() => {
    if (!data) return
    const v = summarize(parsePrometheus(data))
    setView(v)

    const now = performance.now()
    if (prev.current) {
      const dt = (now - prev.current.t) / 1000
      if (dt > 0) {
        const rate = Math.max(0, (v.totalRequests - prev.current.total) / dt)
        setRateHistory((h) => [...h, rate].slice(-MAX_POINTS))
      }
    }
    prev.current = { t: now, total: v.totalRequests }

    if (v.latency) setLatencyHistory((h) => [...h, v.latency!.p99].slice(-MAX_POINTS))
  }, [data])

  return { view, rateHistory, latencyHistory, error, isLoading }
}
