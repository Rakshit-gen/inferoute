import { NextResponse, type NextRequest } from 'next/server'
import { auth } from '@clerk/nextjs/server'
import { addConnection, loadTenant, publicConnection, AuthError } from '@/lib/tenant-store'

export const dynamic = 'force-dynamic'

const json = (status: number, body: unknown) => NextResponse.json(body, { status })

async function guard() {
  const { userId } = await auth()
  if (!userId) throw new AuthError()
}

export async function GET() {
  try {
    await guard()
    const t = await loadTenant()
    return json(200, {
      connections: t.connections.map(publicConnection),
      activeConnectionId: t.activeConnectionId ?? null,
    })
  } catch (e) {
    if (e instanceof AuthError) return json(401, { error: 'sign in required' })
    return json(500, { error: 'could not load connections' })
  }
}

export async function POST(req: NextRequest) {
  try {
    await guard()
    const { name, url, apiKey } = await req.json()
    if (typeof url !== 'string' || !url.trim()) return json(400, { error: 'url is required' })
    const conn = await addConnection({ name: String(name ?? ''), url, apiKey: apiKey ? String(apiKey) : undefined })
    return json(201, publicConnection(conn))
  } catch (e) {
    if (e instanceof AuthError) return json(401, { error: 'sign in required' })
    return json(400, { error: e instanceof Error ? e.message : 'could not add connection' })
  }
}
