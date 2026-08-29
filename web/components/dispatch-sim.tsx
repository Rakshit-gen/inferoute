'use client'

import { useEffect, useMemo, useRef, useState } from 'react'
import { simulate, type Outcome } from '@/lib/sim'
import { ms } from '@/lib/format'

const STAGE_X = [5, 27.5, 50, 72.5, 95] // % across the track, node centres
const STAGES = ['ingress', 'rate-limit', 'cache', 'route', 'origin pool']
const STOP_STAGE: Record<Outcome['kind'], number> = {
  ratelimited: 1,
  cache: 2,
  unavailable: 3,
  served: 4,
}
const COLOR: Record<Outcome['kind'], string> = {
  served: '#c6f24e',
  cache: '#9b8cff',
  ratelimited: '#f5c518',
  unavailable: '#ff5c4d',
}

interface Packet {
  id: number
  p: number // position, 0..4 in stage units
  outcome: Outcome
  fade: number // 1 while travelling, decays after it stops
}

interface Done {
  t: number
  outcome: Outcome
}

const SPEED = 2.6 // stage units per second
const WINDOW_MS = 5000

export function DispatchSim() {
  const [offeredRps, setOfferedRps] = useState(45)
  const [cache, setCache] = useState(true)
  const [rateLimit, setRateLimit] = useState(true)
  const [budgetRps, setBudgetRps] = useState(120)
  const [backends, setBackends] = useState([
    { name: 'ollama-a', healthy: true },
    { name: 'ollama-b', healthy: true },
    { name: 'vllm-c', healthy: true },
  ])

  // Live snapshot the UI renders from; physics runs in refs off the rAF loop.
  const [frame, setFrame] = useState(0)
  const packets = useRef<Packet[]>([])
  const done = useRef<Done[]>([])
  const flash = useRef<number[]>([0, 0, 0, 0, 0])
  const warmth = useRef(0)
  const spawnAcc = useRef(0)
  const nextId = useRef(1)

  const cfg = useRef({ offeredRps, cache, rateLimit, budgetRps, backends })
  cfg.current = { offeredRps, cache, rateLimit, budgetRps, backends }

  const reduced =
    typeof window !== 'undefined' &&
    window.matchMedia('(prefers-reduced-motion: reduce)').matches

  useEffect(() => {
    if (reduced) return
    let raf = 0
    let last = performance.now()

    const loop = (now: number) => {
      const dt = Math.min((now - last) / 1000, 0.1)
      last = now
      const c = cfg.current

      warmth.current = clamp(warmth.current + (c.cache ? dt / 6 : -dt / 2), 0, 1)

      spawnAcc.current += c.offeredRps * dt
      while (spawnAcc.current >= 1 && packets.current.length < 60) {
        spawnAcc.current -= 1
        const outcome = simulate({
          cache: c.cache,
          rateLimit: c.rateLimit,
          offeredRps: c.offeredRps,
          budgetRps: c.budgetRps,
          backends: c.backends,
          cacheWarmth: warmth.current,
        })
        packets.current.push({ id: nextId.current++, p: 0, outcome, fade: 1 })
      }

      for (const pk of packets.current) {
        const stop = STOP_STAGE[pk.outcome.kind]
        if (pk.p < stop) {
          pk.p = Math.min(stop, pk.p + SPEED * dt)
          if (pk.p >= stop) {
            flash.current[stop] = 1
            done.current.push({ t: now, outcome: pk.outcome })
          }
        } else {
          pk.fade -= dt * (pk.outcome.kind === 'served' || pk.outcome.kind === 'cache' ? 3 : 1.6)
        }
      }
      packets.current = packets.current.filter((pk) => pk.fade > 0)
      flash.current = flash.current.map((f) => Math.max(0, f - dt * 2.5))
      done.current = done.current.filter((d) => now - d.t < WINDOW_MS)

      setFrame((f) => (f + 1) % 1_000_000)
      raf = requestAnimationFrame(loop)
    }
    raf = requestAnimationFrame(loop)
    return () => cancelAnimationFrame(raf)
  }, [reduced])

  const stats = useMemo(() => {
    const d = done.current
    const secs = WINDOW_MS / 1000
    const by = (k: Outcome['kind']) => d.filter((x) => x.outcome.kind === k)
    const servedish = d.filter(
      (x) => x.outcome.kind === 'served' || x.outcome.kind === 'cache',
    )
    const lat = servedish.map((x) => x.outcome.latencyMs).sort((a, b) => a - b)
    const perBackend: Record<string, number> = {}
    for (const x of d) if (x.outcome.kind === 'served') perBackend[x.outcome.backend] = (perBackend[x.outcome.backend] ?? 0) + 1
    return {
      servedPerSec: servedish.length / secs,
      cacheRate: servedish.length ? by('cache').length / servedish.length : 0,
      p50: lat.length ? lat[Math.floor(lat.length / 2)] : null,
      dropRate: d.length ? (by('ratelimited').length + by('unavailable').length) / d.length : 0,
      perBackend,
    }
    // frame drives the recompute
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [frame])

  const anyHealthy = backends.some((b) => b.healthy)

  return (
    <div className="panel overflow-hidden">
      <div className="flex flex-wrap items-baseline gap-x-3 gap-y-1 border-b border-line px-4 py-3">
        <span className="eyebrow">dispatch · live simulation</span>
        <span className="text-xs text-ink-dim">
          synthetic traffic — the same path a real request takes through inferoute
        </span>
      </div>

      {/* stat row */}
      <div className="grid grid-cols-2 divide-x divide-line border-b border-line sm:grid-cols-4">
        <Stat label="offered" value={`${offeredRps}/s`} />
        <Stat label="served" value={`${stats.servedPerSec.toFixed(0)}/s`} tone="route" />
        <Stat
          label="cache hit"
          value={cache ? `${(stats.cacheRate * 100).toFixed(0)}%` : 'off'}
          tone="cache"
        />
        <Stat label="p50" value={stats.p50 == null ? '—' : ms(stats.p50)} />
      </div>

      {/* the pipe */}
      <div className="relative mx-4 my-8 h-24">
        <div className="wire absolute left-0 right-0 top-1/2 h-px -translate-y-1/2" />
        {STAGE_X.map((x, i) => {
          const active =
            (i === 1 && rateLimit) ||
            (i === 2 && cache) ||
            i === 0 ||
            i === 3 ||
            (i === 4 && anyHealthy)
          const lit = flash.current[i] ?? 0
          return (
            <div
              key={i}
              className="absolute top-1/2 -translate-x-1/2 -translate-y-1/2"
              style={{ left: `${x}%` }}
            >
              <div
                className={`rounded border bg-panel px-2.5 py-1.5 text-center transition-colors ${
                  active ? 'border-line text-ink' : 'border-line/50 text-ink-dim'
                }`}
                style={
                  lit > 0.02
                    ? { borderColor: flashColor(i, { rateLimit, cache, anyHealthy }), boxShadow: `0 0 ${12 * lit}px ${flashColor(i, { rateLimit, cache, anyHealthy })}` }
                    : undefined
                }
              >
                <div className="font-mono text-[0.7rem] leading-tight">{STAGES[i]}</div>
                <div className="mono-num text-[0.6rem] text-ink-dim">{subFor(i, { rateLimit, budgetRps, cache, backends })}</div>
              </div>
            </div>
          )
        })}

        {!reduced &&
          packets.current.map((pk) => {
            const x = stageToPercent(pk.p)
            return (
              <span
                key={pk.id}
                className="pointer-events-none absolute top-1/2 h-2 w-2 -translate-x-1/2 -translate-y-1/2 rounded-full"
                style={{
                  left: `${x}%`,
                  background: COLOR[pk.outcome.kind],
                  opacity: Math.max(0, Math.min(1, pk.fade)),
                  boxShadow: `0 0 8px ${COLOR[pk.outcome.kind]}`,
                }}
              />
            )
          })}

        {reduced && (
          <p className="absolute inset-x-0 bottom-0 text-center text-[0.65rem] text-ink-dim">
            animation paused (reduced motion)
          </p>
        )}
      </div>

      {/* controls */}
      <div className="space-y-4 border-t border-line px-4 py-4">
        <Slider
          label="offered load"
          value={offeredRps}
          min={1}
          max={220}
          onChange={setOfferedRps}
          suffix=" req/s"
        />

        <div className="flex flex-wrap items-center gap-x-6 gap-y-3">
          <Toggle label="semantic cache" on={cache} onChange={setCache} tone="cache" />
          <Toggle label="rate limit" on={rateLimit} onChange={setRateLimit} tone="warn" />
          {rateLimit && (
            <div className="min-w-[180px] flex-1">
              <Slider
                label="budget"
                value={budgetRps}
                min={10}
                max={220}
                onChange={setBudgetRps}
                suffix=" req/s"
              />
            </div>
          )}
        </div>

        <div>
          <div className="eyebrow mb-2">backends · click to toggle health</div>
          <div className="flex flex-wrap gap-2">
            {backends.map((b, i) => (
              <button
                key={b.name}
                onClick={() =>
                  setBackends((bs) => bs.map((x, j) => (j === i ? { ...x, healthy: !x.healthy } : x)))
                }
                className={`flex items-center gap-1.5 rounded border px-2 py-1 font-mono text-xs transition-colors ${
                  b.healthy ? 'border-line text-ink' : 'border-alert/40 text-ink-dim line-through'
                }`}
              >
                <span className={`led ${b.healthy ? 'bg-route' : 'bg-alert'}`} />
                {b.name}
                <span className="mono-num text-[0.6rem] text-ink-dim">
                  {stats.perBackend[b.name] ?? 0}
                </span>
              </button>
            ))}
            <div className="flex gap-1">
              <button
                onClick={() =>
                  setBackends((bs) =>
                    bs.length >= 6 ? bs : [...bs, { name: `node-${bs.length + 1}`, healthy: true }],
                  )
                }
                className="rounded border border-line px-2 py-1 text-xs text-ink-dim hover:text-ink"
              >
                + add
              </button>
              <button
                onClick={() => setBackends((bs) => (bs.length > 1 ? bs.slice(0, -1) : bs))}
                className="rounded border border-line px-2 py-1 text-xs text-ink-dim hover:text-ink"
              >
                − remove
              </button>
            </div>
          </div>
          {!anyHealthy && (
            <p className="mt-2 text-xs text-alert">
              every backend is down — the gateway now returns 503 until one recovers.
            </p>
          )}
        </div>
      </div>
    </div>
  )
}

function stageToPercent(p: number) {
  const i = Math.min(STAGE_X.length - 2, Math.floor(p))
  const frac = p - i
  return STAGE_X[i] + (STAGE_X[i + 1] - STAGE_X[i]) * frac
}

function flashColor(
  i: number,
  s: { rateLimit: boolean; cache: boolean; anyHealthy: boolean },
) {
  if (i === 1) return COLOR.ratelimited
  if (i === 2) return COLOR.cache
  if (i === 3) return s.anyHealthy ? COLOR.served : COLOR.unavailable
  return COLOR.served
}

function subFor(
  i: number,
  s: { rateLimit: boolean; budgetRps: number; cache: boolean; backends: { healthy: boolean }[] },
) {
  if (i === 0) return 'POST /v1/chat'
  if (i === 1) return s.rateLimit ? `≤ ${s.budgetRps}/s` : 'off'
  if (i === 2) return s.cache ? 'NuclaDB' : 'off'
  if (i === 3) return 'round-robin'
  return `${s.backends.filter((b) => b.healthy).length}/${s.backends.length} up`
}

function Stat({ label, value, tone }: { label: string; value: string; tone?: 'route' | 'cache' }) {
  const c = tone === 'route' ? 'text-route' : tone === 'cache' ? 'text-cache' : 'text-ink'
  return (
    <div className="px-4 py-3">
      <div className="text-[0.62rem] uppercase tracking-widest text-ink-dim">{label}</div>
      <div className={`mono-num text-lg ${c}`}>{value}</div>
    </div>
  )
}

function Slider({
  label,
  value,
  min,
  max,
  onChange,
  suffix = '',
}: {
  label: string
  value: number
  min: number
  max: number
  onChange: (n: number) => void
  suffix?: string
}) {
  return (
    <label className="block">
      <span className="mb-1 flex items-baseline justify-between text-xs text-ink-dim">
        <span className="eyebrow">{label}</span>
        <span className="mono-num text-ink">
          {value}
          {suffix}
        </span>
      </span>
      <input
        type="range"
        min={min}
        max={max}
        value={value}
        onChange={(e) => onChange(Number(e.target.value))}
        className="w-full accent-route"
      />
    </label>
  )
}

function Toggle({
  label,
  on,
  onChange,
  tone,
}: {
  label: string
  on: boolean
  onChange: (b: boolean) => void
  tone: 'cache' | 'warn'
}) {
  const c = tone === 'cache' ? 'bg-cache' : 'bg-warn'
  return (
    <button
      onClick={() => onChange(!on)}
      className="flex items-center gap-2 text-sm text-ink"
      aria-pressed={on}
    >
      <span
        className={`relative h-4 w-7 rounded-full border border-line transition-colors ${
          on ? c : 'bg-panel-2'
        }`}
      >
        <span
          className={`absolute top-0.5 h-2.5 w-2.5 rounded-full bg-void transition-all ${
            on ? 'left-3.5' : 'left-0.5'
          }`}
        />
      </span>
      {label}
    </button>
  )
}

function clamp(n: number, lo: number, hi: number) {
  return n < lo ? lo : n > hi ? hi : n
}
