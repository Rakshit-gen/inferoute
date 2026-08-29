// The browser never talks to a gateway directly. Every call goes to this
// app's own /api/gateway/* routes, which attach the signed-in tenant's
// connection + key server-side. See lib/gateway-proxy.ts.

export interface Backend {
  name: string
  url: string
  models: string[] | null
  healthy: boolean
}

export interface GatewayConfig {
  model_aliases: Record<string, string> | null
  health_check_interval: string
  rate_limit: { enabled: boolean; requests_per_second: number; burst: number }
  cache: { enabled: boolean; max_distance: number }
}

/** Thrown when the tenant has no active gateway connection (HTTP 409). */
export class NoGatewayError extends Error {
  constructor() {
    super('no gateway connected')
    this.name = 'NoGatewayError'
  }
}

async function getJSON<T>(path: string): Promise<T> {
  const res = await fetch(`/api/gateway${path}`, { cache: 'no-store' })
  if (res.status === 409) throw new NoGatewayError()
  if (!res.ok) throw new Error(`${path} → ${res.status}`)
  return res.json() as Promise<T>
}

export const fetchBackends = () => getJSON<Backend[]>('/backends')
export const fetchConfig = () => getJSON<GatewayConfig>('/config')

export async function fetchMetricsText(): Promise<string> {
  const res = await fetch('/api/gateway/metrics', { cache: 'no-store' })
  if (res.status === 409) throw new NoGatewayError()
  if (!res.ok) throw new Error(`/metrics → ${res.status}`)
  return res.text()
}

export interface ChatResult {
  status: number
  servedBy: string | null
  cacheHit: boolean
  latencyMs: number
  body: string
}

/** Sends one completion through the tenant's gateway and reports the routing. */
export async function sendChat(
  model: string,
  prompt: string,
  signal?: AbortSignal,
): Promise<ChatResult> {
  const started = performance.now()
  const res = await fetch('/api/gateway/chat', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ model, messages: [{ role: 'user', content: prompt }] }),
    signal,
  })
  const body = await res.text()
  return {
    status: res.status,
    servedBy: res.headers.get('X-Inferoute-Backend'),
    cacheHit: res.headers.get('X-Inferoute-Cache') === 'hit',
    latencyMs: performance.now() - started,
    body,
  }
}
