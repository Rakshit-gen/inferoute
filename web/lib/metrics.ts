// Minimal Prometheus text-exposition parser, scoped to the three metric
// families inferoute exposes. Not a general implementation: it handles the
// `name{label="v",...} value` line shape and nothing exotic.

export interface Sample {
  name: string
  labels: Record<string, string>
  value: number
}

const LINE = /^([a-zA-Z_:][a-zA-Z0-9_:]*)(\{([^}]*)\})?\s+([0-9eE.+-]+|NaN)$/

function parseLabels(raw: string | undefined): Record<string, string> {
  const out: Record<string, string> = {}
  if (!raw) return out
  for (const m of raw.matchAll(/([a-zA-Z_][a-zA-Z0-9_]*)="((?:[^"\\]|\\.)*)"/g)) {
    out[m[1]] = m[2].replace(/\\"/g, '"').replace(/\\\\/g, '\\').replace(/\\n/g, '\n')
  }
  return out
}

export function parsePrometheus(text: string): Sample[] {
  const samples: Sample[] = []
  for (const line of text.split('\n')) {
    const trimmed = line.trim()
    if (!trimmed || trimmed.startsWith('#')) continue
    const m = LINE.exec(trimmed)
    if (!m) continue
    const value = m[4] === 'NaN' ? NaN : Number(m[4])
    samples.push({ name: m[1], labels: parseLabels(m[3]), value })
  }
  return samples
}

export interface RequestRow {
  model: string
  backend: string
  status: string
  count: number
}

export interface MetricsView {
  totalRequests: number
  requests: RequestRow[]
  byStatus: { status: string; count: number }[]
  byBackend: { backend: string; count: number }[]
  errorRate: number
  cache: { hit: number; miss: number; error: number; hitRate: number }
  latency: { p50: number; p90: number; p99: number; avg: number } | null
}

const BUCKETS_METRIC = 'inferoute_request_duration_seconds_bucket'
const SUM_METRIC = 'inferoute_request_duration_seconds_sum'
const COUNT_METRIC = 'inferoute_request_duration_seconds_count'

/** Linear-interpolated quantile over cumulative histogram buckets. */
export function histogramQuantile(
  buckets: { le: number; count: number }[],
  q: number,
): number {
  const sorted = [...buckets].sort((a, b) => a.le - b.le)
  const total = sorted[sorted.length - 1]?.count ?? 0
  if (total === 0) return 0
  const rank = q * total
  let prevLe = 0
  let prevCount = 0
  for (const b of sorted) {
    if (b.count >= rank) {
      if (!isFinite(b.le)) return prevLe
      const span = b.count - prevCount
      if (span === 0) return b.le
      return prevLe + ((rank - prevCount) / span) * (b.le - prevLe)
    }
    prevLe = isFinite(b.le) ? b.le : prevLe
    prevCount = b.count
  }
  return prevLe
}

export function summarize(samples: Sample[]): MetricsView {
  const requests: RequestRow[] = samples
    .filter((s) => s.name === 'inferoute_requests_total')
    .map((s) => ({
      model: s.labels.model ?? '?',
      backend: s.labels.backend ?? '?',
      status: s.labels.status ?? '?',
      count: s.value,
    }))

  const totalRequests = requests.reduce((n, r) => n + r.count, 0)

  const group = (key: (r: RequestRow) => string) => {
    const m = new Map<string, number>()
    for (const r of requests) m.set(key(r), (m.get(key(r)) ?? 0) + r.count)
    return [...m.entries()]
      .map(([k, count]) => ({ key: k, count }))
      .sort((a, b) => b.count - a.count)
  }

  const byStatus = group((r) => r.status).map((x) => ({ status: x.key, count: x.count }))
  const byBackend = group((r) => r.backend).map((x) => ({ backend: x.key, count: x.count }))

  const errors = requests
    .filter((r) => Number(r.status) >= 400)
    .reduce((n, r) => n + r.count, 0)
  const errorRate = totalRequests > 0 ? errors / totalRequests : 0

  const cacheOf = (outcome: string) =>
    samples
      .filter((s) => s.name === 'inferoute_cache_lookups_total' && s.labels.outcome === outcome)
      .reduce((n, s) => n + s.value, 0)
  const hit = cacheOf('hit')
  const miss = cacheOf('miss')
  const error = cacheOf('error')
  const cacheTotal = hit + miss + error
  const cache = { hit, miss, error, hitRate: cacheTotal > 0 ? hit / cacheTotal : 0 }

  const bucketMap = new Map<number, number>()
  for (const s of samples.filter((s) => s.name === BUCKETS_METRIC)) {
    const le = s.labels.le === '+Inf' ? Infinity : Number(s.labels.le)
    bucketMap.set(le, (bucketMap.get(le) ?? 0) + s.value)
  }
  const buckets = [...bucketMap.entries()].map(([le, count]) => ({ le, count }))
  const sum = samples.filter((s) => s.name === SUM_METRIC).reduce((n, s) => n + s.value, 0)
  const count = samples.filter((s) => s.name === COUNT_METRIC).reduce((n, s) => n + s.value, 0)

  const latency =
    count > 0
      ? {
          p50: histogramQuantile(buckets, 0.5) * 1000,
          p90: histogramQuantile(buckets, 0.9) * 1000,
          p99: histogramQuantile(buckets, 0.99) * 1000,
          avg: (sum / count) * 1000,
        }
      : null

  return { totalRequests, requests, byStatus, byBackend, errorRate, cache, latency }
}
