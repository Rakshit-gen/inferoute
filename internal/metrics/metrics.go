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
		Buckets: []float64{.05, .1, .25, .5, 1, 2.5, 5, 10, 20, 30, 60, 120, 300},
	}, []string{"model"})

	Failovers = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "inferoute_failovers_total",
		Help: "Failed attempts against a backend that led to a retry or an error.",
	}, []string{"backend"})

	Rejected = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "inferoute_rejected_requests_total",
		Help: "Requests refused by the gateway, by reason: unauthorized, rate_limited, or overloaded.",
	}, []string{"reason"})

	CacheWrites = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "inferoute_cache_writes_total",
		Help: "Semantic cache writes by outcome: stored, error, or dropped.",
	}, []string{"outcome"})

	CacheLookups = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "inferoute_cache_lookups_total",
		Help: "Semantic cache lookups by outcome: hit, miss, error, or skipped.",
	}, []string{"outcome"})
)

// ObserveDuration records how long a request took for the given model.
func ObserveDuration(model string, start time.Time) {
	RequestDuration.WithLabelValues(model).Observe(time.Since(start).Seconds())
}

var (
	backendUpDesc = prometheus.NewDesc("inferoute_backend_up",
		"Whether the backend can currently take traffic (1) or not (0).", []string{"backend"}, nil)
	backendInflightDesc = prometheus.NewDesc("inferoute_backend_inflight_requests",
		"Requests currently being proxied to the backend.", []string{"backend"}, nil)
)

// BackendState is one backend as seen at scrape time.
type BackendState struct {
	Name     string
	Up       bool
	Inflight int64
}

type backendCollector func() []BackendState

func (backendCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- backendUpDesc
	ch <- backendInflightDesc
}

func (c backendCollector) Collect(ch chan<- prometheus.Metric) {
	for _, b := range c() {
		up := 0.0
		if b.Up {
			up = 1
		}
		ch <- prometheus.MustNewConstMetric(backendUpDesc, prometheus.GaugeValue, up, b.Name)
		ch <- prometheus.MustNewConstMetric(backendInflightDesc, prometheus.GaugeValue, float64(b.Inflight), b.Name)
	}
}

// RegisterBackends exports the states returned by fn on every scrape.
func RegisterBackends(fn func() []BackendState) { prometheus.MustRegister(backendCollector(fn)) }
