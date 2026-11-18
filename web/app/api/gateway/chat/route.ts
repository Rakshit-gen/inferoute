import { NextResponse, type NextRequest } from 'next/server'
import { withGateway, gatewayFetch } from '@/lib/gateway-proxy'
import { readLimited } from '@/lib/guards'

export const dynamic = 'force-dynamic'

// Headers the dashboard reads off a completion to draw the dispatch trace.
const PASS_THROUGH = ['x-inferoute-backend', 'x-inferoute-cache']

// Longer than the gateway's own upstream_timeout default. It only covers the
// wait for headers, so a long stream is not cut off.
const HEADERS_TIMEOUT_MS = 330_000
const MAX_CHAT_BYTES = 1 << 20

export function POST(req: NextRequest) {
  return withGateway(async (conn) => {
    const payload = await readLimited(req.body, MAX_CHAT_BYTES, 'message is too large', 413)
    const abort = new AbortController()
    req.signal.addEventListener('abort', () => abort.abort(), { once: true })
    const timer = setTimeout(() => abort.abort(), HEADERS_TIMEOUT_MS)
    const upstream = await gatewayFetch(conn, '/v1/chat/completions', {
      method: 'POST',
      headers: { 'content-type': 'application/json' },
      body: payload,
      signal: abort.signal,
    }).finally(() => clearTimeout(timer))
    const headers = new Headers({
      'content-type': upstream.headers.get('content-type') ?? 'application/json',
    })
    for (const h of PASS_THROUGH) {
      const v = upstream.headers.get(h)
      if (v) headers.set(h, v)
    }
    // Pass the body straight through so a streamed (SSE) completion reaches
    // the browser token by token instead of being buffered here.
    return new NextResponse(upstream.body, { status: upstream.status, headers })
  })
}
