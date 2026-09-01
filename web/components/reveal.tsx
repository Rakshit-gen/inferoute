'use client'

import { useEffect, useRef, useState, type CSSProperties, type ReactNode } from 'react'

/**
 * Fades a block up the first time it scrolls into view. `delay` staggers
 * siblings. Falls back to shown when IntersectionObserver is missing, and the
 * globals.css reduced-motion block flattens it. Descendants marked `.r-item`
 * stagger in once this wrapper is in view.
 */
export function Reveal({
  children,
  delay = 0,
  className = '',
}: {
  children: ReactNode
  delay?: number
  className?: string
}) {
  const ref = useRef<HTMLDivElement>(null)
  const [shown, setShown] = useState(false)

  useEffect(() => {
    const el = ref.current
    if (!el || typeof IntersectionObserver === 'undefined') {
      setShown(true)
      return
    }
    const io = new IntersectionObserver(
      ([e]) => {
        if (e.isIntersecting) {
          setShown(true)
          io.disconnect()
        }
      },
      { rootMargin: '0px 0px -12% 0px' },
    )
    io.observe(el)
    return () => io.disconnect()
  }, [])

  return (
    <div
      ref={ref}
      className={`reveal ${shown ? 'is-in' : ''} ${className}`}
      style={delay ? ({ '--d': `${delay}s` } as CSSProperties) : undefined}
    >
      {children}
    </div>
  )
}
