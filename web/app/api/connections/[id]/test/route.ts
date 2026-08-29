import { NextResponse, type NextRequest } from 'next/server'
import { auth } from '@clerk/nextjs/server'
import { getConnection, authFor, AuthError } from '@/lib/tenant-store'

export const dynamic = 'force-dynamic'

const json = (status: number, body: unknown) => NextResponse.json(body, { status })

// Probes one of the caller's connections: is the gateway reachable, and
// does its API key work? Used by the "Test" button on the Connections page.
export async function GET(_req: NextRequest, { params }: { params: { id: string } }) {
  try {
    const { userId } = await auth()
    if (!userId) throw new AuthError()

    const conn = await getConnection(params.id)
    if (!conn) return json(404, { error: 'unknown connection' })

    const started = Date.now()
    let res: Response
    try {
      res = await fetch(`${conn.url}/v1/config`, {
        headers: authFor(conn),
        cache: 'no-store',
        signal: AbortSignal.timeout(8000),
      })
    } catch (e) {
      return json(200, {
        reachable: false,
        error: e instanceof Error ? e.message : 'could not connect',
      })
    }

    const latencyMs = Date.now() - started
    if (res.status === 401 || res.status === 403) {
      return json(200, { reachable: true, authOk: false, status: res.status, latencyMs })
    }
    if (!res.ok) {
      return json(200, { reachable: true, authOk: true, status: res.status, latencyMs })
    }
    const config = await res.json().catch(() => null)
    return json(200, { reachable: true, authOk: true, status: 200, latencyMs, config })
  } catch (e) {
    if (e instanceof AuthError) return json(401, { error: 'sign in required' })
    return json(500, { error: 'probe failed' })
  }
}
