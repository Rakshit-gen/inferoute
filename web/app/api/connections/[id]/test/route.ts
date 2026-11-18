import { NextResponse, type NextRequest } from 'next/server'
import { auth } from '@clerk/nextjs/server'
import { apiError, gatewayFetch, MAX_GATEWAY_RESPONSE } from '@/lib/gateway-proxy'
import { readLimited, ValidationError } from '@/lib/guards'
import { getConnection, AuthError } from '@/lib/tenant-store'

export const dynamic = 'force-dynamic'

const json = (status: number, body: unknown) => NextResponse.json(body, { status })

// Probes one of the caller's connections: is the gateway reachable, and
// does its API key work? Used by the "Test" button on the Connections page.
export async function GET(_req: NextRequest, { params }: { params: Promise<{ id: string }> }) {
  try {
    const { userId } = await auth()
    if (!userId) throw new AuthError()

    const conn = await getConnection((await params).id)
    if (!conn) return json(404, { error: 'unknown connection' })

    const started = Date.now()
    let res: Response
    try {
      res = await gatewayFetch(conn, '/v1/config', { signal: AbortSignal.timeout(8000) })
    } catch (e) {
      return json(200, {
        reachable: false,
        error: e instanceof ValidationError ? e.message : 'could not connect',
      })
    }

    const latencyMs = Date.now() - started
    if (res.status === 401 || res.status === 403) {
      return json(200, { reachable: true, authOk: false, status: res.status, latencyMs })
    }
    if (!res.ok) {
      return json(200, { reachable: true, authOk: true, status: res.status, latencyMs })
    }
    const config = await readLimited(res.body, MAX_GATEWAY_RESPONSE, 'too large', 502)
      .then(JSON.parse)
      .catch(() => null)
    return json(200, { reachable: true, authOk: true, status: 200, latencyMs, config })
  } catch (e) {
    return apiError(e, 'probe failed')
  }
}
