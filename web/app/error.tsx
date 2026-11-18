'use client'

export default function ErrorPage({ reset }: { error: Error; reset: () => void }) {
  return (
    <main className="mx-auto max-w-xl px-4 py-24 text-center">
      <h1 className="text-xl text-ink">Something went wrong</h1>
      <p className="mt-2 text-sm text-ink-dim">This page hit an error. Your connections are not affected.</p>
      <button onClick={reset} className="mt-6 text-sm text-route hover:underline">
        Try again
      </button>
    </main>
  )
}
