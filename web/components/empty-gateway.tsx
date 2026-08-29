import Link from 'next/link'

/** Shown on data pages when the tenant has no active gateway connection. */
export function EmptyGateway() {
  return (
    <div className="panel p-10 text-center">
      <div className="eyebrow mb-2">no gateway connected</div>
      <p className="mx-auto max-w-sm text-sm text-ink-dim">
        Register an inferoute gateway to see its backends, traffic, and cache here. Your
        connections are private to your account.
      </p>
      <Link
        href="/connections"
        className="mt-4 inline-block rounded bg-route px-3 py-2 font-display text-sm font-semibold text-void"
      >
        Add a connection
      </Link>
    </div>
  )
}
