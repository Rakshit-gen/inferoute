'use client'

import Link from 'next/link'
import { usePathname } from 'next/navigation'
import { useQuery } from '@tanstack/react-query'
import { SignedIn, SignedOut, SignInButton, SignUpButton, UserButton, useAuth } from '@clerk/nextjs'
import { fetchMetricsText } from '@/lib/api'
import { Logo } from '@/components/logo'
import { parsePrometheus, summarize } from '@/lib/metrics'
import { useEffect, useRef, useState } from 'react'

const links = [
  { href: '/dashboard', label: 'Dashboard' },
  { href: '/playground', label: 'Playground' },
  { href: '/connections', label: 'Connections' },
  { href: '/settings', label: 'Settings' },
  { href: '/docs', label: 'Docs' },
]

const GITHUB = 'https://github.com/Rakshit-gen/inferoute'

function useReqRate(enabled: boolean) {
  const { data } = useQuery({
    queryKey: ['metrics-nav'],
    queryFn: fetchMetricsText,
    refetchInterval: 4000,
    enabled,
    retry: false,
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
  const { isSignedIn } = useAuth()
  const onLivePage = pathname.startsWith('/dashboard') || pathname.startsWith('/playground')
  const rate = useReqRate(Boolean(isSignedIn) && onLivePage)
  const [menuOpen, setMenuOpen] = useState(false)

  // close the mobile menu on route change
  useEffect(() => {
    setMenuOpen(false)
  }, [pathname])

  return (
    <header className="relative z-20 border-b border-line bg-panel/60 backdrop-blur">
      <div className="mx-auto flex max-w-6xl items-center gap-4 px-4 py-3 sm:gap-6">
        <Link href="/" className="flex shrink-0 items-center gap-2 font-display text-lg font-bold tracking-tight text-ink">
          <Logo className="h-6 w-6" />
          infer<span className="text-route">oute</span>
        </Link>
        <SignedIn>
          <nav className="hidden gap-1 md:flex">
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
        </SignedIn>
        <SignedOut>
          <Link href="/docs" className="hidden rounded px-2.5 py-1 text-sm text-ink-dim hover:text-ink sm:block">
            Docs
          </Link>
        </SignedOut>

        <div className="ml-auto flex items-center gap-3 text-xs text-ink-dim sm:gap-4">
          {isSignedIn && onLivePage && (
            <span className="mono-num hidden sm:inline">
              {rate === null ? '—' : rate.toFixed(rate < 10 ? 1 : 0)} req/s
            </span>
          )}
          <a href={GITHUB} target="_blank" rel="noreferrer" className="hidden hover:text-ink sm:inline">
            GitHub ↗
          </a>
          <SignedOut>
            <SignInButton mode="modal" forceRedirectUrl="/dashboard">
              <button className="rounded px-2 py-1 text-sm text-ink-dim hover:text-ink sm:px-2.5">Sign in</button>
            </SignInButton>
            <SignUpButton mode="modal" forceRedirectUrl="/dashboard">
              <button className="rounded bg-route px-3 py-1.5 font-display text-sm font-semibold text-void">
                Sign up
              </button>
            </SignUpButton>
          </SignedOut>
          <SignedIn>
            <UserButton afterSignOutUrl="/" />
            <button
              onClick={() => setMenuOpen((v) => !v)}
              aria-label={menuOpen ? 'Close menu' : 'Open menu'}
              aria-expanded={menuOpen}
              className="-mr-1 rounded p-1 text-ink-dim hover:text-ink md:hidden"
            >
              <svg viewBox="0 0 20 20" className="h-5 w-5" fill="none" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round">
                {menuOpen ? (
                  <>
                    <path d="M5 5l10 10" />
                    <path d="M15 5L5 15" />
                  </>
                ) : (
                  <>
                    <path d="M3 6h14" />
                    <path d="M3 10h14" />
                    <path d="M3 14h14" />
                  </>
                )}
              </svg>
            </button>
          </SignedIn>
        </div>
      </div>

      <SignedIn>
        {menuOpen && (
          <nav className="border-t border-line bg-panel px-3 py-2 md:hidden">
            {links.map((l) => {
              const active = pathname === l.href || pathname.startsWith(l.href + '/')
              return (
                <Link
                  key={l.href}
                  href={l.href}
                  className={`block rounded px-3 py-2 text-sm transition-colors ${
                    active ? 'bg-panel-2 text-ink' : 'text-ink-dim hover:text-ink'
                  }`}
                >
                  {l.label}
                </Link>
              )
            })}
            <a
              href={GITHUB}
              target="_blank"
              rel="noreferrer"
              className="block rounded px-3 py-2 text-sm text-ink-dim hover:text-ink"
            >
              GitHub ↗
            </a>
          </nav>
        )}
      </SignedIn>
    </header>
  )
}
