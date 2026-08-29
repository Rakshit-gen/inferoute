export const pct = (x: number) => `${(x * 100).toFixed(1)}%`

export const ms = (x: number) => {
  if (x < 1) return `${x.toFixed(2)}ms`
  if (x < 100) return `${x.toFixed(1)}ms`
  if (x < 10_000) return `${Math.round(x)}ms`
  return `${(x / 1000).toFixed(1)}s`
}

export const num = (x: number) => {
  if (x < 1000) return String(Math.round(x))
  if (x < 1_000_000) return `${(x / 1000).toFixed(x < 10_000 ? 1 : 0)}k`
  return `${(x / 1_000_000).toFixed(1)}M`
}

export const statusColor = (status: string) => {
  const n = Number(status)
  if (n >= 500) return 'var(--alert)'
  if (n >= 400) return 'var(--warn)'
  if (n >= 200 && n < 300) return 'var(--route)'
  return 'var(--ink-dim)'
}
