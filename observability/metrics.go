package observability

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	// RequestDuration tracks request latency
	RequestDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "scalemind_request_duration_seconds",
			Help:    "Duration of tool execution requests in seconds",
			Buckets: []float64{0.1, 0.25, 0.5, 1.0, 2.5, 5.0},
		},
		[]string{"tool", "provider", "status"},
	)

	// RequestTotal tracks total requests
	RequestTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "scalemind_requests_total",
			Help: "Total number of tool execution requests",
		},
		[]string{"tool", "provider", "status"},
	)

	// ScalingOperations tracks scaling operations
	ScalingOperations = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "scalemind_scaling_operations_total",
			Help: "Total number of scaling operations",
		},
		[]string{"provider", "type", "status"},
	)

	// ErrorRate tracks error rates
	ErrorRate = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "scalemind_errors_total",
			Help: "Total number of errors",
		},
		[]string{"tool", "provider", "error_type"},
	)
)

// InitializeMetrics registers all Prometheus metrics
func InitializeMetrics() {
	prometheus.MustRegister(RequestDuration)
	prometheus.MustRegister(RequestTotal)
	prometheus.MustRegister(ScalingOperations)
	prometheus.MustRegister(ErrorRate)
}

// MetricsHandler returns HTTP handler for Prometheus metrics
func MetricsHandler() http.Handler {
	return promhttp.Handler()
}

