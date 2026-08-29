// Pure outcome model for the landing-page dispatch simulation. Mirrors what
// inferouted actually does to a request, in the same order: rate-limit gate,
// semantic cache, then round-robin across healthy backends with failover.

export interface SimConfig {
  cache: boolean
  rateLimit: boolean
  offeredRps: number // current synthetic load
  budgetRps: number // rate-limit ceiling
  backends: { name: string; healthy: boolean }[]
  cacheWarmth: number // 0..1, raises hit probability as the cache fills
}

export type Outcome =
  | { kind: 'ratelimited'; latencyMs: number }
  | { kind: 'cache'; latencyMs: number }
  | { kind: 'served'; backend: string; latencyMs: number }
  | { kind: 'unavailable'; latencyMs: number }

export function simulate(cfg: SimConfig, rand: () => number = Math.random): Outcome {
  if (cfg.rateLimit && cfg.offeredRps > cfg.budgetRps) {
    const over = (cfg.offeredRps - cfg.budgetRps) / cfg.offeredRps
    if (rand() < over) return { kind: 'ratelimited', latencyMs: 1 + rand() * 2 }
  }

  if (cfg.cache && rand() < 0.12 + cfg.cacheWarmth * 0.55) {
    return { kind: 'cache', latencyMs: 5 + rand() * 12 }
  }

  const healthy = cfg.backends.filter((b) => b.healthy)
  if (healthy.length === 0) return { kind: 'unavailable', latencyMs: 20 + rand() * 30 }

  const backend = healthy[Math.floor(rand() * healthy.length)].name
  return { kind: 'served', backend, latencyMs: 160 + rand() * 340 }
}
