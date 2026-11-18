const SAFE_METHODS = new Set(['GET', 'HEAD', 'OPTIONS'])

/** False when a browser sent a state-changing request from another site. */
export function isSameOrigin(method: string, origin: string | null, host: string | null): boolean {
  if (SAFE_METHODS.has(method) || !origin) return true
  try {
    return new URL(origin).host === host?.split(',')[0].trim()
  } catch {
    return false // includes the opaque "null" origin
  }
}
