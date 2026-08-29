// Self-check for the Prometheus parser + histogram math. Run: npm run check
import assert from 'node:assert/strict'
import { parsePrometheus, summarize, histogramQuantile } from './metrics'

const text = `
# HELP inferoute_requests_total x
# TYPE inferoute_requests_total counter
inferoute_requests_total{backend="ollama-east",model="llama3",status="200"} 8
inferoute_requests_total{backend="ollama-west",model="llama3",status="200"} 6
inferoute_requests_total{backend="ollama-east",model="mistral",status="500"} 2
inferoute_cache_lookups_total{outcome="hit"} 3
inferoute_cache_lookups_total{outcome="miss"} 7
inferoute_request_duration_seconds_bucket{model="llama3",le="0.1"} 0
inferoute_request_duration_seconds_bucket{model="llama3",le="0.5"} 5
inferoute_request_duration_seconds_bucket{model="llama3",le="1"} 15
inferoute_request_duration_seconds_bucket{model="llama3",le="+Inf"} 16
inferoute_request_duration_seconds_sum{model="llama3"} 9.6
inferoute_request_duration_seconds_count{model="llama3"} 16
`

const v = summarize(parsePrometheus(text))
assert.equal(v.totalRequests, 16)
assert.equal(v.byBackend[0].backend, 'ollama-east') // 10 beats 6
assert.equal(v.byBackend[0].count, 10)
assert.equal(v.errorRate, 2 / 16)
assert.equal(v.cache.hit, 3)
assert.ok(Math.abs(v.cache.hitRate - 0.3) < 1e-9)
assert.ok(v.latency && Math.abs(v.latency.avg - 600) < 1, `avg ${v.latency?.avg}`)
// p50: rank 8 lands in (0.5,1] -> 0.5 + (8-5)/10 * 0.5 = 0.65s
assert.ok(Math.abs(v.latency!.p50 - 650) < 1, `p50 ${v.latency!.p50}`)
// p99: rank 15.84 > bucket le=1 (count 15), next is +Inf -> clamp to last finite le
assert.ok(Math.abs(v.latency!.p99 - 1000) < 1, `p99 ${v.latency!.p99}`)

// empty histogram -> 0, not NaN
assert.equal(histogramQuantile([{ le: 1, count: 0 }, { le: Infinity, count: 0 }], 0.9), 0)

// label-value escaping
const [s] = parsePrometheus('x{a="b\\"c"} 1')
assert.equal(s.labels.a, 'b"c')

console.log('metrics.check: OK')
