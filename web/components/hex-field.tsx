'use client'

import { useEffect, useRef } from 'react'

interface Pt {
  x: number
  y: number
}
interface Edge {
  a: Pt
  b: Pt
  aKey: string
  bKey: string
}
interface Runner {
  edge: Edge
  fromKey: string
  t: number
  speed: number
  color: string
  trail: number
}

const R = 34 // hex circumradius, px
const BASE_ALPHA = 0.05
const LIME = '#c6f24e'
const VIOLET = '#9b8cff'

/**
 * Ambient background: a faint hex lattice with a handful of light points
 * tracing its edges vertex to vertex, leaving a short glowing trail.
 * Canvas, one rAF loop, respects prefers-reduced-motion.
 */
export function HexField() {
  const ref = useRef<HTMLCanvasElement>(null)

  useEffect(() => {
    const cv = ref.current
    if (!cv) return
    const g0 = cv.getContext("2d")
    if (!g0) return
    const cv2: HTMLCanvasElement = cv
    const g: CanvasRenderingContext2D = g0

    const reduce = window.matchMedia('(prefers-reduced-motion: reduce)').matches

    let edges: Edge[] = []
    let adj = new Map<string, Edge[]>()
    let base = new Path2D()
    let runners: Runner[] = []
    let dpr = 1
    let w = 0
    let h = 0
    let raf = 0
    let last = performance.now()

    const vkey = (x: number, y: number) => `${Math.round(x * 2)}:${Math.round(y * 2)}`
    const link = (k: string, e: Edge) => {
      const l = adj.get(k)
      if (l) l.push(e)
      else adj.set(k, [e])
    }
    const lerp = (from: Pt, to: Pt, t: number): Pt => ({
      x: from.x + (to.x - from.x) * t,
      y: from.y + (to.y - from.y) * t,
    })
    const clamp01 = (t: number) => (t < 0 ? 0 : t > 1 ? 1 : t)

    function buildGrid() {
      edges = []
      adj = new Map()
      base = new Path2D()
      const seen = new Set<string>()
      const stepX = R * 1.5
      const stepY = Math.sqrt(3) * R
      const cols = Math.ceil(w / stepX) + 2
      const rows = Math.ceil(h / stepY) + 2
      for (let c = -1; c < cols; c++) {
        for (let r = -1; r < rows; r++) {
          const cx = c * stepX
          const cy = r * stepY + (c & 1 ? stepY / 2 : 0)
          const verts: Pt[] = []
          for (let i = 0; i < 6; i++) {
            const ang = (Math.PI / 3) * i
            verts.push({ x: cx + R * Math.cos(ang), y: cy + R * Math.sin(ang) })
          }
          for (let i = 0; i < 6; i++) {
            const a = verts[i]
            const b = verts[(i + 1) % 6]
            const ak = vkey(a.x, a.y)
            const bk = vkey(b.x, b.y)
            const ek = ak < bk ? `${ak}|${bk}` : `${bk}|${ak}`
            if (seen.has(ek)) continue
            seen.add(ek)
            const edge: Edge = { a, b, aKey: ak, bKey: bk }
            edges.push(edge)
            base.moveTo(a.x, a.y)
            base.lineTo(b.x, b.y)
            link(ak, edge)
            link(bk, edge)
          }
        }
      }
    }

    function spawn(): Runner {
      const edge = edges[(Math.random() * edges.length) | 0]
      return {
        edge,
        fromKey: Math.random() < 0.5 ? edge.aKey : edge.bKey,
        t: Math.random(),
        speed: 0.22 + Math.random() * 0.5,
        color: Math.random() < 0.22 ? VIOLET : LIME,
        trail: 0.45 + Math.random() * 0.8,
      }
    }

    function resize() {
      dpr = Math.min(window.devicePixelRatio || 1, 2)
      w = window.innerWidth
      h = window.innerHeight
      cv2.width = Math.floor(w * dpr)
      cv2.height = Math.floor(h * dpr)
      cv2.style.width = `${w}px`
      cv2.style.height = `${h}px`
      buildGrid()
      const count = Math.max(6, Math.min(24, Math.round((w * h) / 95000)))
      runners = Array.from({ length: count }, spawn)
      if (reduce) drawStatic()
    }

    function drawBase() {
      g.setTransform(dpr, 0, 0, dpr, 0, 0)
      g.clearRect(0, 0, w, h)
      g.strokeStyle = `rgba(232, 237, 234, ${BASE_ALPHA})`
      g.lineWidth = 1
      g.stroke(base)
    }

    function drawStatic() {
      drawBase()
      g.globalCompositeOperation = 'lighter'
      for (let i = 0; i < 40; i++) {
        const e = edges[(Math.random() * edges.length) | 0]
        const p = lerp(e.a, e.b, Math.random())
        g.fillStyle = Math.random() < 0.22 ? VIOLET : LIME
        g.globalAlpha = 0.5
        g.beginPath()
        g.arc(p.x, p.y, 1.4, 0, Math.PI * 2)
        g.fill()
      }
      g.globalAlpha = 1
      g.globalCompositeOperation = 'source-over'
    }

    function step(dt: number) {
      drawBase()
      g.globalCompositeOperation = 'lighter'
      g.lineCap = 'round'

      for (const rn of runners) {
        rn.t += rn.speed * dt
        if (rn.t >= 1) {
          const arrived = rn.fromKey === rn.edge.aKey ? rn.edge.bKey : rn.edge.aKey
          const opts = (adj.get(arrived) ?? []).filter((e) => e !== rn.edge)
          rn.edge = opts.length ? opts[(Math.random() * opts.length) | 0] : rn.edge
          rn.fromKey = arrived
          rn.t -= 1
        }
        const from = rn.fromKey === rn.edge.aKey ? rn.edge.a : rn.edge.b
        const to = rn.fromKey === rn.edge.aKey ? rn.edge.b : rn.edge.a
        const head = lerp(from, to, clamp01(rn.t))
        const tail = lerp(from, to, clamp01(rn.t - rn.trail))

        const grad = g.createLinearGradient(tail.x, tail.y, head.x, head.y)
        grad.addColorStop(0, `${rn.color}00`)
        grad.addColorStop(1, rn.color)
        g.strokeStyle = grad
        g.lineWidth = 1.6
        g.beginPath()
        g.moveTo(tail.x, tail.y)
        g.lineTo(head.x, head.y)
        g.stroke()

        g.fillStyle = rn.color
        g.shadowColor = rn.color
        g.shadowBlur = 8
        g.beginPath()
        g.arc(head.x, head.y, 1.7, 0, Math.PI * 2)
        g.fill()
        g.shadowBlur = 0
      }
      g.globalCompositeOperation = 'source-over'
    }

    function frame(now: number) {
      const dt = Math.min((now - last) / 1000, 0.05)
      last = now
      step(dt)
      raf = requestAnimationFrame(frame)
    }

    resize()
    let resizeTimer = 0
    const onResize = () => {
      window.clearTimeout(resizeTimer)
      resizeTimer = window.setTimeout(resize, 150)
    }
    window.addEventListener('resize', onResize)
    if (!reduce) raf = requestAnimationFrame(frame)

    return () => {
      cancelAnimationFrame(raf)
      window.clearTimeout(resizeTimer)
      window.removeEventListener('resize', onResize)
    }
  }, [])

  return (
    <canvas
      ref={ref}
      aria-hidden="true"
      className="pointer-events-none fixed inset-0 -z-10"
      style={{ opacity: 0.9 }}
    />
  )
}
