package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	RequestsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "llm_requests_total",
		Help: "Total number of LLM requests.",
	}, []string{"app", "provider", "model", "status"})

	RequestDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "llm_request_duration_seconds",
		Help:    "Request duration in seconds.",
		Buckets: prometheus.DefBuckets,
	}, []string{"app", "provider", "model", "stream"})

	TokensTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "llm_tokens_total",
		Help: "Total tokens processed.",
	}, []string{"app", "provider", "model", "direction"})

	StreamFirstByte = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "llm_stream_first_byte_seconds",
		Help:    "Time to first byte for streaming requests.",
		Buckets: prometheus.DefBuckets,
	}, []string{"app", "provider", "model"})

	ProviderErrors = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "llm_provider_errors_total",
		Help: "Total provider errors.",
	}, []string{"provider", "error_type"})
)
