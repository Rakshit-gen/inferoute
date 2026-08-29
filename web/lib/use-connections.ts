'use client'

import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'

export interface PublicConnection {
  id: string
  name: string
  url: string
  hasApiKey: boolean
}

export interface ProbeResult {
  reachable: boolean
  authOk?: boolean
  status?: number
  latencyMs?: number
  error?: string
  config?: {
    api_keys_required?: boolean
    rate_limit?: { enabled: boolean }
    cache?: { enabled: boolean }
  } | null
}

interface ConnectionsResponse {
  connections: PublicConnection[]
  activeConnectionId: string | null
}

async function jsonFetch(url: string, init?: RequestInit) {
  const res = await fetch(url, init)
  const body = await res.json().catch(() => ({}))
  if (!res.ok) throw new Error(body.error || `${url} -> ${res.status}`)
  return body
}

export function useConnections() {
  const qc = useQueryClient()
  const query = useQuery<ConnectionsResponse>({
    queryKey: ['connections'],
    queryFn: () => jsonFetch('/api/connections'),
    retry: false,
  })

  const invalidate = () => {
    qc.invalidateQueries({ queryKey: ['connections'] })
    // active connection changed -> every gateway-backed view is now stale
    qc.invalidateQueries({ queryKey: ['backends'] })
    qc.invalidateQueries({ queryKey: ['config'] })
    qc.invalidateQueries({ queryKey: ['metrics'] })
  }

  const add = useMutation({
    mutationFn: (input: { name: string; url: string; apiKey?: string }) =>
      jsonFetch('/api/connections', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(input),
      }),
    onSuccess: invalidate,
  })

  const update = useMutation({
    mutationFn: ({
      id,
      ...patch
    }: {
      id: string
      name?: string
      url?: string
      apiKey?: string | null
    }) =>
      jsonFetch(`/api/connections/${id}`, {
        method: 'PATCH',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(patch),
      }),
    onSuccess: invalidate,
  })

  const remove = useMutation({
    mutationFn: (id: string) => jsonFetch(`/api/connections/${id}`, { method: 'DELETE' }),
    onSuccess: invalidate,
  })

  const setActive = useMutation({
    mutationFn: (id: string) =>
      jsonFetch(`/api/connections/${id}`, {
        method: 'PATCH',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ active: true }),
      }),
    onSuccess: invalidate,
  })

  const test = useMutation<ProbeResult, Error, string>({
    mutationFn: (id: string) => jsonFetch(`/api/connections/${id}/test`),
  })

  const connections = query.data?.connections ?? []
  const activeConnectionId = query.data?.activeConnectionId ?? null
  const active = connections.find((c) => c.id === activeConnectionId) ?? null

  return { ...query, connections, activeConnectionId, active, add, update, remove, setActive, test }
}
