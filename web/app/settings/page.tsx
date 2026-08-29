'use client'

import { UserProfile } from '@clerk/nextjs'
import { useConnections } from '@/lib/use-connections'

export default function SettingsPage() {
  const { connections, remove } = useConnections()

  return (
    <div className="space-y-8">
      <div>
        <h1 className="font-display text-2xl font-semibold tracking-tight">Settings</h1>
        <p className="text-sm text-ink-dim">Your account, and the gateways it can reach.</p>
      </div>

      <section>
        <div className="eyebrow mb-3">account</div>
        <div className="overflow-hidden rounded border border-line">
          <UserProfile routing="hash" />
        </div>
      </section>

      <section>
        <div className="eyebrow mb-3">danger zone</div>
        <div className="panel border-alert/30 p-4">
          <div className="flex flex-wrap items-center justify-between gap-3">
            <div>
              <div className="text-sm text-ink">Remove all gateway connections</div>
              <p className="text-xs text-ink-dim">
                {connections.length} connection{connections.length === 1 ? '' : 's'} stored. This does
                not touch the gateways themselves.
              </p>
            </div>
            <button
              disabled={connections.length === 0 || remove.isPending}
              onClick={() => {
                if (confirm('Remove all gateway connections from your account?')) {
                  connections.forEach((c) => remove.mutate(c.id))
                }
              }}
              className="rounded border border-alert/40 px-3 py-1.5 text-xs text-alert hover:bg-alert/10 disabled:opacity-40"
            >
              Remove all
            </button>
          </div>
        </div>
        <p className="mt-2 text-xs text-ink-dim">
          To delete your account entirely, use the account panel above.
        </p>
      </section>
    </div>
  )
}
