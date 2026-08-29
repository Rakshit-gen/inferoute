import { NextResponse, type NextRequest } from 'next/server'
import { auth } from '@clerk/nextjs/server'
import { removeConnection, setActiveConnection, AuthError } from '@/lib/tenant-store'

export const dynamic = 'force-dynamic'

const json = (status: number, body: unknown) => NextResponse.json(body, { status })

async function guard() {
  const { userId } = await auth()
  if (!userId) throw new AuthError()
}

type Ctx = { params: { id: string } }

export async function DELETE(_req: NextRequest, { params }: Ctx) {
  try {
    await guard()
    await removeConnection(params.id)
    return json(200, { ok: true })
  } catch (e) {
    if (e instanceof AuthError) return json(401, { error: 'sign in required' })
    return json(400, { error: e instanceof Error ? e.message : 'could not remove connection' })
  }
}

// Sets this connection as the active one for the tenant.
export async function PATCH(_req: NextRequest, { params }: Ctx) {
  try {
    await guard()
    await setActiveConnection(params.id)
    return json(200, { ok: true })
  } catch (e) {
    if (e instanceof AuthError) return json(401, { error: 'sign in required' })
    return json(400, { error: e instanceof Error ? e.message : 'could not switch connection' })
  }
}
