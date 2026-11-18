/** @type {import('next').NextConfig} */
const securityHeaders = [
  { key: 'X-Frame-Options', value: 'DENY' },
  { key: 'X-Content-Type-Options', value: 'nosniff' },
  { key: 'Referrer-Policy', value: 'strict-origin-when-cross-origin' },
  { key: 'Strict-Transport-Security', value: 'max-age=31536000' },
  { key: 'Permissions-Policy', value: 'camera=(), microphone=(), geolocation=()' },
]

module.exports = {
  poweredByHeader: false,
  headers: async () => [{ source: '/:path*', headers: securityHeaders }],
  // Not a static export anymore: Clerk auth + the per-tenant BFF routes
  // under app/api/* need the Node runtime. Still deploys to Vercel as-is.
  env: {
    // Optional: pre-fills the "add connection" form in dev.
    NEXT_PUBLIC_DEFAULT_GATEWAY_URL: process.env.NEXT_PUBLIC_DEFAULT_GATEWAY_URL || '',
  },
}
