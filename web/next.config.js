/** @type {import('next').NextConfig} */
module.exports = {
  // Static export: the dashboard is a pure client that talks to a running
  // inferouted over its HTTP API. Deploy the `out/` dir anywhere static.
  output: 'export',
  images: { unoptimized: true },
  env: {
    NEXT_PUBLIC_INFEROUTE_URL:
      process.env.NEXT_PUBLIC_INFEROUTE_URL || 'http://localhost:8081',
  },
}
