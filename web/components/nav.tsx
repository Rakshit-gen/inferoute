'use client'

import Link from 'next/link'
import { usePathname } from 'next/navigation'
import { useQuery } from '@tanstack/react-query'
import { fetchMetricsText, GATEWAY_URL } from '@/lib/api'
import { parsePrometheus, summarize } from '@/lib/metrics'
import { useEffect, useRef, useState } from 'react'

const links = [
  { href: '/dashboard', label: 'Dashboard' },
  { href: '/playground', label: 'Playground' },
  { href: '/docs', label: 'Docs' },
]

const GITHUB = 'https://github.com/Rakshit-gen/inferoute'

function useReqRate(enabled: boolean) {
  const { data } = useQuery({
    queryKey: ['metrics-nav'],
    queryFn: fetchMetricsText,
    refetchInterval: 4000,
    enabled,
  })
  const prev = useRef<{ t: number; total: number } | null>(null)
  const [rate, setRate] = useState<number | null>(null)

  useEffect(() => {
    if (!data) return
    const total = summarize(parsePrometheus(data)).totalRequests
    const now = performance.now()
    if (prev.current) {
      const dt = (now - prev.current.t) / 1000
      if (dt > 0) setRate(Math.max(0, (total - prev.current.total) / dt))
    }
    prev.current = { t: now, total }
  }, [data])

  return rate
}

export function Nav() {
  const pathname = usePathname()
  const live = pathname.startsWith('/dashboard') || pathname.startsWith('/playground')
  const rate = useReqRate(live)

  return (
    <header className="border-b border-line bg-panel/60 backdrop-blur">
      <div className="mx-auto flex max-w-6xl items-center gap-6 px-4 py-3">
        <Link href="/" className="font-display text-lg font-bold tracking-tight text-ink">
          infer<span className="text-route">oute</span>
        </Link>
        <nav className="flex gap-1">
          {links.map((l) => {
            const active = pathname === l.href || pathname.startsWith(l.href + '/')
            return (
              <Link
                key={l.href}
                href={l.href}
                className={`rounded px-2.5 py-1 text-sm transition-colors ${
                  active ? 'bg-panel-2 text-ink' : 'text-ink-dim hover:text-ink'
                }`}
              >
                {l.label}
              </Link>
            )
          })}
        </nav>
        <div className="ml-auto flex items-center gap-4 text-xs text-ink-dim">
          {live && (
            <span className="mono-num">
              {rate === null ? '—' : rate.toFixed(rate < 10 ? 1 : 0)} req/s
            </span>
          )}
          <a href={GITHUB} target="_blank" rel="noreferrer" className="hidden hover:text-ink sm:inline">
            GitHub ↗
          </a>
        </div>
      </div>
    </header>
  )
}
