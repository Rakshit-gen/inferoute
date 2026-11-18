import 'server-only'
import { NextResponse } from 'next/server'
import { auth } from '@clerk/nextjs/server'
import { assertPublicGateway, readLimited, ValidationError } from './guards'
import { activeConnection, authFor, AuthError, type Connection } from './tenant-store'

export const MAX_GATEWAY_RESPONSE = 8 << 20

const json = (status: number, body: unknown) => NextResponse.json(body, { status })

/** Turns a thrown error into a response that reveals nothing internal. */
export function apiError(e: unknown, fallback: string, status = 500): Response {
  if (e instanceof AuthError) return json(401, { error: 'sign in required' })
  if (e instanceof ValidationError) return json(e.status, { error: e.message })
  console.error(`${fallback}:`, e)
  return json(status, { error: fallback })
}

/**
 * Runs `fn` with the caller's own active gateway connection. This is the
 * tenant-isolation boundary: `activeConnection()` only ever resolves a
 * connection from the signed-in tenant's list, so one tenant's request can
 * never be proxied to another tenant's gateway.
 */
export async function withGateway(
  fn: (conn: Connection) => Promise<Response>,
): Promise<Response> {
  try {
    const { userId } = await auth()
    if (!userId) return json(401, { error: 'sign in required' })
    const conn = await activeConnection()
    if (!conn) return json(409, { error: 'no gateway connected' })
    return await fn(conn)
  } catch (e) {
    return apiError(e, 'gateway unreachable', 502)
  }
}

/** The only way this app calls a gateway: checks the target every time. */
export async function gatewayFetch(conn: Connection, path: string, init: RequestInit = {}): Promise<Response> {
  await assertPublicGateway(conn.url)
  return fetch(`${conn.url}${path}`, {
    ...init,
    headers: { ...(init.headers as Record<string, string> | undefined), ...authFor(conn) },
    cache: 'no-store',
    redirect: 'error', // a redirect could point past the address check
  })
}

/** Proxies a GET to the connection's gateway, passing the body + status through. */
export async function proxyGet(conn: Connection, path: string): Promise<Response> {
  const upstream = await gatewayFetch(conn, path, { signal: AbortSignal.timeout(10_000) })
  const body = await readLimited(upstream.body, MAX_GATEWAY_RESPONSE, 'gateway response is too large', 502)
  return new NextResponse(body, {
    status: upstream.status,
    headers: { 'content-type': upstream.headers.get('content-type') ?? 'application/json' },
  })
}
