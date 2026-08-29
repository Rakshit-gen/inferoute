import 'server-only'
import { NextResponse } from 'next/server'
import { auth } from '@clerk/nextjs/server'
import { activeConnection, AuthError, type Connection } from './tenant-store'

const json = (status: number, body: unknown) => NextResponse.json(body, { status })

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
    if (e instanceof AuthError) return json(401, { error: 'sign in required' })
    return json(502, { error: e instanceof Error ? e.message : 'gateway unreachable' })
  }
}

export function authHeaders(conn: Connection): HeadersInit {
  return conn.apiKey ? { Authorization: `Bearer ${conn.apiKey}` } : {}
}

/** Proxies a GET to the connection's gateway, passing the body + status through. */
export async function proxyGet(conn: Connection, path: string): Promise<Response> {
  const upstream = await fetch(`${conn.url}${path}`, {
    headers: authHeaders(conn),
    cache: 'no-store',
  })
  const body = await upstream.text()
  return new NextResponse(body, {
    status: upstream.status,
    headers: { 'content-type': upstream.headers.get('content-type') ?? 'application/json' },
  })
}
