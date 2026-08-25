// Package metrics holds the Prometheus collectors inferoute exposes on
// /metrics: request volume/latency by model and outcome, and cache hit
// rate.
package metrics

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	RequestsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "inferoute_requests_total",
		Help: "Total proxied requests by model, backend, and outcome.",
	}, []string{"model", "backend", "status"})

	RequestDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "inferoute_request_duration_seconds",
		Help:    "End-to-end request latency by model, from receipt to fully written response.",
		Buckets: prometheus.DefBuckets,
	}, []string{"model"})

	CacheLookups = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "inferoute_cache_lookups_total",
		Help: "Semantic cache lookups by outcome: hit, miss, or error.",
	}, []string{"outcome"})
)

// ObserveDuration records how long a request took for the given model.
func ObserveDuration(model string, start time.Time) {
	RequestDuration.WithLabelValues(model).Observe(time.Since(start).Seconds())
}
