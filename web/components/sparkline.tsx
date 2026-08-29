'use client'

export function Sparkline({
  points,
  color = 'var(--route)',
  height = 40,
  width = 240,
}: {
  points: number[]
  color?: string
  height?: number
  width?: number
}) {
  if (points.length < 2) {
    return <div style={{ height }} className="flex items-center text-xs text-ink-dim">collecting…</div>
  }
  const max = Math.max(...points, 1)
  const min = Math.min(...points, 0)
  const span = max - min || 1
  const step = width / (points.length - 1)
  const d = points
    .map((p, i) => {
      const x = i * step
      const y = height - ((p - min) / span) * (height - 4) - 2
      return `${i === 0 ? 'M' : 'L'}${x.toFixed(1)},${y.toFixed(1)}`
    })
    .join(' ')

  return (
    <svg width={width} height={height} className="w-full" viewBox={`0 0 ${width} ${height}`} preserveAspectRatio="none">
      <path d={`${d} L${width},${height} L0,${height} Z`} fill={color} opacity={0.12} />
      <path d={d} fill="none" stroke={color} strokeWidth={1.5} vectorEffect="non-scaling-stroke" />
    </svg>
  )
}
