import 'server-only'
import { randomUUID } from 'crypto'
import { auth, clerkClient } from '@clerk/nextjs/server'

export interface Connection {
  id: string
  name: string
  url: string // base URL of an inferoute gateway
  apiKey?: string // sent as Authorization: Bearer to that gateway
}

export interface TenantData {
  connections: Connection[]
  activeConnectionId?: string
}

export class AuthError extends Error {}

/**
 * The tenant is the active Clerk organization when there is one, otherwise
 * the signed-in user. Data lives in that entity's privateMetadata, which
 * `auth()` + `clerkClient` only ever expose for the current caller, so
 * tenant A physically cannot read tenant B's connections or reach B's
 * gateway.
 */
async function tenantRef(): Promise<{ userId: string; orgId?: string }> {
  const { userId, orgId } = await auth()
  if (!userId) throw new AuthError('not signed in')
  return { userId, orgId: orgId ?? undefined }
}

const empty: TenantData = { connections: [] }

export async function loadTenant(): Promise<TenantData> {
  const { userId, orgId } = await tenantRef()
  const client = await clerkClient()
  const md = orgId
    ? (await client.organizations.getOrganization({ organizationId: orgId })).privateMetadata
    : (await client.users.getUser(userId)).privateMetadata
  const raw = (md?.inferoute as TenantData | undefined) ?? empty
  return { connections: raw.connections ?? [], activeConnectionId: raw.activeConnectionId }
}

async function saveTenant(data: TenantData): Promise<void> {
  const { userId, orgId } = await tenantRef()
  const client = await clerkClient()
  if (orgId) {
    await client.organizations.updateOrganizationMetadata(orgId, { privateMetadata: { inferoute: data } })
  } else {
    await client.users.updateUserMetadata(userId, { privateMetadata: { inferoute: data } })
  }
}

export async function addConnection(input: {
  name: string
  url: string
  apiKey?: string
}): Promise<Connection> {
  const url = input.url.trim().replace(/\/$/, '')
  if (!/^https?:\/\//.test(url)) throw new Error('url must start with http:// or https://')
  const data = await loadTenant()
  const conn: Connection = {
    id: randomUUID(),
    name: input.name.trim() || 'gateway',
    url,
    apiKey: input.apiKey?.trim() || undefined,
  }
  data.connections.push(conn)
  if (!data.activeConnectionId) data.activeConnectionId = conn.id
  await saveTenant(data)
  return conn
}

export async function updateConnection(
  id: string,
  patch: { name?: string; url?: string; apiKey?: string | null },
): Promise<Connection> {
  const data = await loadTenant()
  const conn = data.connections.find((c) => c.id === id)
  if (!conn) throw new Error('unknown connection')
  if (patch.name !== undefined) conn.name = patch.name.trim() || conn.name
  if (patch.url !== undefined) {
    const url = patch.url.trim().replace(/\/$/, '')
    if (!/^https?:\/\//.test(url)) throw new Error('url must start with http:// or https://')
    conn.url = url
  }
  // null clears the key; undefined leaves it; a string sets it
  if (patch.apiKey === null) conn.apiKey = undefined
  else if (typeof patch.apiKey === 'string' && patch.apiKey.trim()) conn.apiKey = patch.apiKey.trim()
  await saveTenant(data)
  return conn
}

export async function removeConnection(id: string): Promise<void> {
  const data = await loadTenant()
  data.connections = data.connections.filter((c) => c.id !== id)
  if (data.activeConnectionId === id) data.activeConnectionId = data.connections[0]?.id
  await saveTenant(data)
}

export async function setActiveConnection(id: string): Promise<void> {
  const data = await loadTenant()
  if (!data.connections.some((c) => c.id === id)) throw new Error('unknown connection')
  data.activeConnectionId = id
  await saveTenant(data)
}

/** One of the caller's own connections by id, or null. Never another tenant's. */
export async function getConnection(id: string): Promise<Connection | null> {
  const data = await loadTenant()
  return data.connections.find((c) => c.id === id) ?? null
}

/**
 * Resolves which of the caller's connections to read from. An id that is
 * not in this tenant's own list resolves to their active/first connection,
 * never another tenant's. You can only ever address your own.
 */
export async function activeConnection(explicitId?: string | null): Promise<Connection | null> {
  const data = await loadTenant()
  if (explicitId) {
    const owned = data.connections.find((c) => c.id === explicitId)
    if (owned) return owned
  }
  return data.connections.find((c) => c.id === data.activeConnectionId) ?? data.connections[0] ?? null
}

/** Strips the secret before anything reaches the client. */
export function publicConnection(c: Connection) {
  return { id: c.id, name: c.name, url: c.url, hasApiKey: Boolean(c.apiKey) }
}

/** The Authorization header to send to this connection's gateway, if any. */
export function authFor(c: Connection): HeadersInit {
  return c.apiKey ? { Authorization: `Bearer ${c.apiKey}` } : {}
}
