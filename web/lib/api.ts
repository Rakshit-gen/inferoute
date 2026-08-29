export const GATEWAY_URL = (
  process.env.NEXT_PUBLIC_INFEROUTE_URL || 'http://localhost:8081'
).replace(/\/$/, '')

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

async function getJSON<T>(path: string): Promise<T> {
  const res = await fetch(`${GATEWAY_URL}${path}`, { cache: 'no-store' })
  if (!res.ok) throw new Error(`${path} → ${res.status}`)
  return res.json() as Promise<T>
}

export const fetchBackends = () => getJSON<Backend[]>('/v1/backends')
export const fetchConfig = () => getJSON<GatewayConfig>('/v1/config')

export async function fetchMetricsText(): Promise<string> {
  const res = await fetch(`${GATEWAY_URL}/metrics`, { cache: 'no-store' })
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

/** Sends one completion through the gateway and reports how it was routed. */
export async function sendChat(
  model: string,
  prompt: string,
  apiKey: string,
  signal?: AbortSignal,
): Promise<ChatResult> {
  const started = performance.now()
  const headers: Record<string, string> = { 'Content-Type': 'application/json' }
  if (apiKey.trim()) headers['Authorization'] = `Bearer ${apiKey.trim()}`

  const res = await fetch(`${GATEWAY_URL}/v1/chat/completions`, {
    method: 'POST',
    headers,
    body: JSON.stringify({
      model,
      messages: [{ role: 'user', content: prompt }],
    }),
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
