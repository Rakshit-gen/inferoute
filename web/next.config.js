/** @type {import('next').NextConfig} */
module.exports = {
  // Not a static export anymore: Clerk auth + the per-tenant BFF routes
  // under app/api/* need the Node runtime. Still deploys to Vercel as-is.
  env: {
    // Optional: pre-fills the "add connection" form in dev.
    NEXT_PUBLIC_DEFAULT_GATEWAY_URL: process.env.NEXT_PUBLIC_DEFAULT_GATEWAY_URL || '',
  },
}
