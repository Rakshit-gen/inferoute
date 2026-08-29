import { NextResponse, type NextRequest } from 'next/server'
import { withGateway, authHeaders } from '@/lib/gateway-proxy'

export const dynamic = 'force-dynamic'

// Headers the dashboard reads off a completion to draw the dispatch trace.
const PASS_THROUGH = ['x-inferoute-backend', 'x-inferoute-cache']

export function POST(req: NextRequest) {
  return withGateway(async (conn) => {
    const payload = await req.text()
    const upstream = await fetch(`${conn.url}/v1/chat/completions`, {
      method: 'POST',
      headers: { 'content-type': 'application/json', ...authHeaders(conn) },
      body: payload,
      cache: 'no-store',
    })
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
