import { lookup } from 'node:dns/promises'
import { BlockList, isIP } from 'node:net'

/** An error whose message is safe to show the caller. */
export class ValidationError extends Error {
  constructor(
    message: string,
    readonly status = 400,
  ) {
    super(message)
  }
}

const blocked = new BlockList()
for (const [net, bits] of [
  ['0.0.0.0', 8],
  ['10.0.0.0', 8],
  ['100.64.0.0', 10],
  ['127.0.0.0', 8],
  ['169.254.0.0', 16], // includes cloud metadata services
  ['172.16.0.0', 12],
  ['192.0.0.0', 24],
  ['192.168.0.0', 16],
  ['198.18.0.0', 15],
  ['224.0.0.0', 4],
  ['240.0.0.0', 4],
] as const) {
  blocked.addSubnet(net, bits, 'ipv4')
}
for (const [net, bits] of [
  ['::', 128],
  ['::1', 128],
  ['64:ff9b::', 96],
  ['fc00::', 7],
  ['fe80::', 10],
  ['ff00::', 8],
] as const) {
  blocked.addSubnet(net, bits, 'ipv6')
}

export const isPrivateAddress = (ip: string) => blocked.check(ip, isIP(ip) === 6 ? 'ipv6' : 'ipv4')

/** Validates a gateway base URL and returns it without a trailing slash. */
export function gatewayUrl(raw: string): string {
  let url: URL
  try {
    url = new URL(raw.trim())
  } catch {
    throw new ValidationError('url must be a valid http:// or https:// address')
  }
  if (url.protocol !== 'http:' && url.protocol !== 'https:') {
    throw new ValidationError('url must start with http:// or https://')
  }
  if (url.username || url.password) throw new ValidationError('put the key in the API key field, not the url')
  if (url.search || url.hash) throw new ValidationError('url must not have a query string or fragment')
  return (url.origin + url.pathname).replace(/\/+$/, '')
}

type Resolve = (host: string) => Promise<string[]>
const resolveHost: Resolve = async (host) => (await lookup(host, { all: true })).map((a) => a.address)

/**
 * Stops signed-in users from aiming this server at its own network. Private
 * addresses are allowed in development, or with ALLOW_PRIVATE_GATEWAYS=true
 * when gateways really do live on a private network.
 *
 * ponytail: fetch resolves the name again, so a DNS-rebinding host can slip
 * through between the two lookups. Pin the address in a custom dispatcher if
 * this app ever runs next to sensitive unauthenticated services.
 */
export async function assertPublicGateway(
  url: string,
  allowPrivate = process.env.NODE_ENV !== 'production' || process.env.ALLOW_PRIVATE_GATEWAYS === 'true',
  resolve = resolveHost,
): Promise<void> {
  if (allowPrivate) return
  const host = new URL(url).hostname.replace(/^\[|\]$/g, '')
  let addresses = [host]
  if (!isIP(host)) {
    try {
      addresses = await resolve(host)
    } catch {
      throw new ValidationError('gateway host could not be resolved', 502)
    }
  }
  if (addresses.some(isPrivateAddress)) throw new ValidationError('gateway address is not allowed')
}
