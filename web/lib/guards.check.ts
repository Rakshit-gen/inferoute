// Self-check for the request guards. Run: npm run check
import assert from 'node:assert/strict'
import { assertPublicGateway, gatewayUrl, isPrivateAddress, ValidationError } from './guards'

for (const ip of ['127.0.0.1', '10.1.2.3', '172.16.0.1', '192.168.1.1', '169.254.169.254', '::1', 'fe80::1', 'fd00::1', '::ffff:127.0.0.1']) {
  assert.ok(isPrivateAddress(ip), ip)
}
for (const ip of ['8.8.8.8', '172.32.0.1', '2606:4700:4700::1111']) assert.ok(!isPrivateAddress(ip), ip)

assert.equal(gatewayUrl(' https://gw.example/base/ '), 'https://gw.example/base')
for (const bad of ['ftp://x', 'nonsense', 'https://u:p@x', 'https://x?a=1', 'https://x#f']) {
  assert.throws(() => gatewayUrl(bad), ValidationError, bad)
}

const rejects = (url: string, addrs: string[]) =>
  assert.rejects(assertPublicGateway(url, false, async () => addrs), ValidationError, url)

async function main() {
  await rejects('http://127.1', [])
  await rejects('http://[::1]:8081', [])
  await rejects('http://169.254.169.254', [])
  await rejects('https://looks-public.example', ['8.8.8.8', '10.0.0.1'])
  await assertPublicGateway('https://gw.example', false, async () => ['8.8.8.8'])
  await assertPublicGateway('http://localhost:8091', true)
  console.log('guards.check: OK')
}
main()
