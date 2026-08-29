import assert from 'node:assert'
import { simulate, type SimConfig } from './sim'

const base: SimConfig = {
  cache: false,
  rateLimit: false,
  offeredRps: 1,
  budgetRps: 100,
  backends: [{ name: 'a', healthy: true }],
  cacheWarmth: 0,
}

// One healthy backend, no cache, no limit → always served by it.
assert.equal(simulate(base).kind, 'served')

// No healthy backend → unavailable (this is the failover-exhausted case).
assert.equal(simulate({ ...base, backends: [{ name: 'a', healthy: false }] }).kind, 'unavailable')

// Cache on, fully warm, rand→0 → served from cache.
assert.equal(simulate({ ...base, cache: true, cacheWarmth: 1 }, () => 0).kind, 'cache')

// Load far over budget, rand→0 → rate-limited before anything else runs.
assert.equal(
  simulate({ ...base, rateLimit: true, offeredRps: 100, budgetRps: 1 }, () => 0).kind,
  'ratelimited',
)

// Under budget → the limiter never trips.
assert.notEqual(
  simulate({ ...base, rateLimit: true, offeredRps: 1, budgetRps: 100 }, () => 0).kind,
  'ratelimited',
)

console.log('sim.check: OK')
