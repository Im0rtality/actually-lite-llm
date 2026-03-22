package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	RequestsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "llm_requests_total",
		Help: "Total number of LLM requests.",
	}, []string{"virtual_key", "provider", "model", "status"})

	RequestDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "llm_request_duration_seconds",
		Help:    "Request duration in seconds.",
		Buckets: prometheus.DefBuckets,
	}, []string{"virtual_key", "provider", "model", "stream"})

	TokensTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "llm_tokens_total",
		Help: "Total tokens processed.",
	}, []string{"virtual_key", "provider", "model", "direction"})

	StreamFirstByte = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "llm_stream_first_byte_seconds",
		Help:    "Time to first byte for streaming requests.",
		Buckets: prometheus.DefBuckets,
	}, []string{"virtual_key", "provider", "model"})

	ProviderErrors = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "llm_provider_errors_total",
		Help: "Total provider errors.",
	}, []string{"provider", "error_type"})

	CostTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "llm_cost_dollars_total",
		Help: "Estimated cost in US dollars based on config-defined per-model pricing. Only populated for non-streaming requests and models with pricing configured.",
	}, []string{"virtual_key", "provider", "model"})
)
