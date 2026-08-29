import { clerkMiddleware, createRouteMatcher } from '@clerk/nextjs/server'

// Pages that require a signed-in user. The API routes under /api/* run their
// own auth() check and return JSON errors, so they're not gated here.
const isProtectedPage = createRouteMatcher([
  '/dashboard(.*)',
  '/playground(.*)',
  '/connections(.*)',
])

export default clerkMiddleware(async (auth, req) => {
  if (isProtectedPage(req)) await auth.protect()
})

export const config = {
  matcher: [
    '/((?!_next|[^?]*\\.(?:html?|css|js(?!on)|jpe?g|webp|png|gif|svg|ttf|woff2?|ico|csv|docx?|xlsx?|zip|webmanifest)).*)',
    '/(api|trpc)(.*)',
    '/__clerk/(.*)',
  ],
}
