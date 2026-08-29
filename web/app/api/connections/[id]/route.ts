import { NextResponse, type NextRequest } from 'next/server'
import { auth } from '@clerk/nextjs/server'
import {
  removeConnection,
  setActiveConnection,
  updateConnection,
  publicConnection,
  AuthError,
} from '@/lib/tenant-store'

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

// Body { active: true } switches the active connection. Body with any of
// name / url / apiKey edits the connection (apiKey: null clears the key).
export async function PATCH(req: NextRequest, { params }: Ctx) {
  try {
    await guard()
    const body = await req.json().catch(() => ({}))
    if (body.active) {
      await setActiveConnection(params.id)
      return json(200, { ok: true })
    }
    const conn = await updateConnection(params.id, {
      name: body.name,
      url: body.url,
      apiKey: body.apiKey === null ? null : body.apiKey,
    })
    return json(200, publicConnection(conn))
  } catch (e) {
    if (e instanceof AuthError) return json(401, { error: 'sign in required' })
    return json(400, { error: e instanceof Error ? e.message : 'could not update connection' })
  }
}
