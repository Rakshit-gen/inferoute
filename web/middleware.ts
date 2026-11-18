import { clerkMiddleware, createRouteMatcher } from '@clerk/nextjs/server'
import { NextResponse } from 'next/server'
import { isSameOrigin } from './lib/same-origin'

// Pages that require a signed-in user. The API routes under /api/* run their
// own auth() check and return JSON errors, so they're not gated here.
const isProtectedPage = createRouteMatcher([
  '/dashboard(.*)',
  '/playground(.*)',
  '/connections(.*)',
  '/settings(.*)',
])

export default clerkMiddleware(async (auth, req) => {
  const host = req.headers.get('x-forwarded-host') ?? req.headers.get('host')
  if (req.nextUrl.pathname.startsWith('/api/') && !isSameOrigin(req.method, req.headers.get('origin'), host)) {
    return NextResponse.json({ error: 'cross-site request refused' }, { status: 403 })
  }
  if (isProtectedPage(req)) await auth.protect()
})

export const config = {
  matcher: [
    '/((?!_next|[^?]*\\.(?:html?|css|js(?!on)|jpe?g|webp|png|gif|svg|ttf|woff2?|ico|csv|docx?|xlsx?|zip|webmanifest)).*)',
    '/(api|trpc)(.*)',
    '/__clerk/(.*)',
  ],
}
