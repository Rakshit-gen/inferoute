import { withGateway, proxyGet } from '@/lib/gateway-proxy'

export const dynamic = 'force-dynamic'

export const GET = () => withGateway((conn) => proxyGet(conn, '/metrics'))
