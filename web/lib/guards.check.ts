// Self-check for the request guards. Run: npm run check
import assert from 'node:assert/strict'
import { isSameOrigin } from './same-origin'
import { assertPublicGateway, gatewayUrl, isPrivateAddress, readLimited, textField, ValidationError } from './guards'

for (const ip of ['127.0.0.1', '10.1.2.3', '172.16.0.1', '192.168.1.1', '169.254.169.254', '::1', 'fe80::1', 'fd00::1', '::ffff:127.0.0.1']) {
  assert.ok(isPrivateAddress(ip), ip)
}
for (const ip of ['8.8.8.8', '172.32.0.1', '2606:4700:4700::1111']) assert.ok(!isPrivateAddress(ip), ip)

assert.equal(gatewayUrl(' https://gw.example/base/ '), 'https://gw.example/base')
for (const bad of ['ftp://x', 'nonsense', 'https://u:p@x', 'https://x?a=1', 'https://x#f']) {
  assert.throws(() => gatewayUrl(bad), ValidationError, bad)
}

assert.equal(textField(' a ', 'name', 1), 'a')
assert.throws(() => textField('ab', 'name', 1), ValidationError)
assert.throws(() => textField(1, 'name', 1), ValidationError)

assert.ok(isSameOrigin('POST', 'https://app.example', 'app.example'))
assert.ok(isSameOrigin('GET', 'https://evil.example', 'app.example'))
assert.ok(isSameOrigin('POST', null, 'app.example'))
assert.ok(!isSameOrigin('POST', 'https://evil.example', 'app.example'))
assert.ok(!isSameOrigin('DELETE', 'null', 'app.example'))

const rejects = (url: string, addrs: string[]) =>
  assert.rejects(assertPublicGateway(url, false, async () => addrs), ValidationError, url)

async function main() {
  await rejects('http://127.1', [])
  await rejects('http://[::1]:8081', [])
  await rejects('http://169.254.169.254', [])
  await rejects('https://looks-public.example', ['8.8.8.8', '10.0.0.1'])
  await assertPublicGateway('https://gw.example', false, async () => ['8.8.8.8'])
  await assertPublicGateway('http://localhost:8091', true)
  const body = () => new Response('héllo').body
  assert.equal(await readLimited(body(), 6, 'big', 413), 'héllo')
  await assert.rejects(readLimited(body(), 5, 'big', 413), (e: ValidationError) => e.status === 413)
  console.log('guards.check: OK')
}
main()
