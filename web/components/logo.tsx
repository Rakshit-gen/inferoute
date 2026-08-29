/**
 * The inferoute mark: a track switch. One request enters at the left, reaches
 * the switch (the diamond), and is sent down the taken route (lime) instead of
 * the diverging one (dim). That is the whole product in one glyph.
 */
export function Logo({ className }: { className?: string }) {
  return (
    <svg
      viewBox="0 0 24 24"
      fill="none"
      className={className}
      strokeLinecap="square"
      strokeLinejoin="miter"
      aria-hidden="true"
    >
      {/* entry node */}
      <rect x="1.5" y="10.5" width="3" height="3" fill="currentColor" />
      {/* trunk into the switch */}
      <path d="M4.5 12h1.2" stroke="currentColor" strokeWidth="2.2" />
      {/* the switch */}
      <path d="M9 8.5 12.5 12 9 15.5 5.5 12Z" fill="var(--void)" stroke="currentColor" strokeWidth="1.7" />
      {/* diverging route, not taken */}
      <path d="M12.5 12H15l4 5.5" stroke="currentColor" strokeWidth="2.2" opacity="0.32" />
      <rect x="19" y="16" width="3" height="3" fill="currentColor" opacity="0.32" />
      {/* taken route */}
      <path d="M12.5 12H15l4-5.5" stroke="var(--route)" strokeWidth="2.2" />
      <rect x="19" y="5" width="3" height="3" fill="var(--route)" />
    </svg>
  )
}
