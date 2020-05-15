package eksconfig

import (
	"github.com/aws/aws-k8s-tester/pkg/metrics"
)

// RequestsSummary represents request results.
type RequestsSummary struct {
	// SuccessTotal is the number of successful client requests.
	SuccessTotal float64 `json:"success-total" read-only:"true"`
	// FailureTotal is the number of failed client requests.
	FailureTotal float64 `json:"failure-total" read-only:"true"`
	// LatencyHistogram is the client requests latency histogram.
	LatencyHistogram metrics.HistogramBuckets `json:"latency-histogram,omitempty" read-only:"true"`
}