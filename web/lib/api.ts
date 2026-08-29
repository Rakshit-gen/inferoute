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
  ttfbMs: number | null
  streamed: boolean
  body: string
  /** Assistant text, assembled from streamed deltas or the final JSON. */
  content: string | null
}

interface SendChatOpts {
  stream?: boolean
  signal?: AbortSignal
  /** Called with the running assistant text as streamed deltas arrive. */
  onProgress?: (content: string) => void
}

function contentFromJSON(body: string): string | null {
  try {
    const j = JSON.parse(body)
    return j.message?.content ?? j.choices?.[0]?.message?.content ?? null
  } catch {
    return null
  }
}

/** Sends one completion through the tenant's gateway and reports the routing. */
export async function sendChat(
  model: string,
  prompt: string,
  opts: SendChatOpts = {},
): Promise<ChatResult> {
  const { stream = false, signal, onProgress } = opts
  const started = performance.now()
  const res = await fetch('/api/gateway/chat', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ model, stream, messages: [{ role: 'user', content: prompt }] }),
    signal,
  })

  const meta = {
    status: res.status,
    servedBy: res.headers.get('X-Inferoute-Backend'),
    cacheHit: res.headers.get('X-Inferoute-Cache') === 'hit',
  }
  const isSSE = (res.headers.get('content-type') ?? '').includes('text/event-stream')

  if (!isSSE || !res.body) {
    const body = await res.text()
    return {
      ...meta,
      latencyMs: performance.now() - started,
      ttfbMs: null,
      streamed: false,
      body,
      content: contentFromJSON(body),
    }
  }

  const reader = res.body.pipeThrough(new TextDecoderStream()).getReader()
  let raw = ''
  let content = ''
  let ttfbMs: number | null = null
  let buffer = ''

  for (;;) {
    const { done, value } = await reader.read()
    if (done) break
    if (ttfbMs === null) ttfbMs = performance.now() - started
    raw += value
    buffer += value
    const lines = buffer.split('\n')
    buffer = lines.pop() ?? ''
    for (const line of lines) {
      const trimmed = line.trim()
      if (!trimmed.startsWith('data:')) continue
      const data = trimmed.slice(5).trim()
      if (data === '[DONE]') continue
      try {
        const delta = JSON.parse(data).choices?.[0]?.delta?.content
        if (delta) {
          content += delta
          onProgress?.(content)
        }
      } catch {
        /* ignore keep-alives and partial frames */
      }
    }
  }

  return {
    ...meta,
    latencyMs: performance.now() - started,
    ttfbMs,
    streamed: true,
    body: raw,
    content: content || null,
  }
}
