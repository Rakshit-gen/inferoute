'use client'

import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'

export interface PublicConnection {
  id: string
  name: string
  url: string
  hasApiKey: boolean
}

interface ConnectionsResponse {
  connections: PublicConnection[]
  activeConnectionId: string | null
}

async function jsonFetch(url: string, init?: RequestInit) {
  const res = await fetch(url, init)
  const body = await res.json().catch(() => ({}))
  if (!res.ok) throw new Error(body.error || `${url} → ${res.status}`)
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
    // active connection changed → every gateway-backed view is now stale
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

  const remove = useMutation({
    mutationFn: (id: string) => jsonFetch(`/api/connections/${id}`, { method: 'DELETE' }),
    onSuccess: invalidate,
  })

  const setActive = useMutation({
    mutationFn: (id: string) => jsonFetch(`/api/connections/${id}`, { method: 'PATCH' }),
    onSuccess: invalidate,
  })

  const connections = query.data?.connections ?? []
  const activeConnectionId = query.data?.activeConnectionId ?? null
  const active = connections.find((c) => c.id === activeConnectionId) ?? null

  return { ...query, connections, activeConnectionId, active, add, remove, setActive }
}
