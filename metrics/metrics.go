package metrics

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// ===============================
// TX PIPELINE (end-to-end)
// ===============================
var (
	TxCreateDuration = prometheus.NewHistogram(prometheus.HistogramOpts{
		Namespace: "blockchain",
		Subsystem: "tx",
		Name:      "create_duration_ms",
		Help:      "Time spent creating a transaction",
		Buckets:   prometheus.ExponentialBuckets(0.1, 2, 15),
	})

	TxVerifyMempoolDuration = prometheus.NewHistogram(prometheus.HistogramOpts{
		Namespace: "blockchain",
		Subsystem: "tx",
		Name:      "verify_mempool_duration_ms",
		Help:      "Time spent verifying transaction for mempool",
		Buckets:   prometheus.ExponentialBuckets(0.1, 2, 15),
	})

	TxAddMempoolDuration = prometheus.NewHistogram(prometheus.HistogramOpts{
		Namespace: "blockchain",
		Subsystem: "tx",
		Name:      "add_mempool_duration_ms",
		Help:      "Time spent adding transaction to mempool",
		Buckets:   prometheus.ExponentialBuckets(0.05, 2, 15),
	})
	FnDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: "blockchain",
			Subsystem: "fn",
			Name:      "duration_ms",
			Help:      "Function execution duration",
			Buckets:   prometheus.ExponentialBuckets(0.05, 2, 15),
		},
		[]string{"name"},
	)
)

// ===============================
// MEMPOOL (điểm nghi ngờ CHÍNH)
// ===============================
var (
	MempoolIsSpentDuration = prometheus.NewHistogram(prometheus.HistogramOpts{
		Namespace: "blockchain",
		Subsystem: "mempool",
		Name:      "is_spent_duration_ms",
		Help:      "Time spent checking mempool spent-index",
		Buckets:   prometheus.ExponentialBuckets(0.01, 2, 15),
	})

	MempoolHasOutputDuration = prometheus.NewHistogram(prometheus.HistogramOpts{
		Namespace: "blockchain",
		Subsystem: "mempool",
		Name:      "has_output_duration_ms",
		Help:      "Time spent checking mempool output existence",
		Buckets:   prometheus.ExponentialBuckets(0.01, 2, 15),
	})

	MempoolFindOutputsDuration = prometheus.NewHistogram(prometheus.HistogramOpts{
		Namespace: "blockchain",
		Subsystem: "mempool",
		Name:      "find_outputs_by_address_duration_ms",
		Help:      "Time spent scanning mempool outputs by address (SHOULD BE LOW)",
		Buckets:   prometheus.ExponentialBuckets(0.1, 2, 15),
	})

	MempoolSize = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: "blockchain",
		Subsystem: "mempool",
		Name:      "size",
		Help:      "Current number of transactions in mempool",
	})
)

// ===============================
// CRYPTO (CPU HEAVY)
// ===============================
var (
	TxSignDuration = prometheus.NewHistogram(prometheus.HistogramOpts{
		Namespace: "blockchain",
		Subsystem: "crypto",
		Name:      "sign_duration_ms",
		Help:      "Time spent signing transaction",
		Buckets:   prometheus.ExponentialBuckets(0.1, 2, 15),
	})

	TxVerifySigDuration = prometheus.NewHistogram(prometheus.HistogramOpts{
		Namespace: "blockchain",
		Subsystem: "crypto",
		Name:      "verify_signature_duration_ms",
		Help:      "Time spent verifying ed25519 signatures",
		Buckets:   prometheus.ExponentialBuckets(0.1, 2, 15),
	})
)

// ===============================
// REDIS (ROUND-TRIP COST)
// ===============================
var (
	RedisGetDuration = prometheus.NewHistogram(prometheus.HistogramOpts{
		Namespace: "blockchain",
		Subsystem: "redis",
		Name:      "get_duration_ms",
		Help:      "Redis GET latency",
		Buckets:   prometheus.ExponentialBuckets(0.01, 2, 15),
	})

	RedisPipelineDuration = prometheus.NewHistogram(prometheus.HistogramOpts{
		Namespace: "blockchain",
		Subsystem: "redis",
		Name:      "pipeline_duration_ms",
		Help:      "Redis pipeline execution latency",
		Buckets:   prometheus.ExponentialBuckets(0.05, 2, 15),
	})
)

// ===============================
// REGISTER ALL
// ===============================
func Register() {
	prometheus.MustRegister(
		TxCreateDuration,
		TxVerifyMempoolDuration,
		TxAddMempoolDuration,
		FnDuration,

		MempoolIsSpentDuration,
		MempoolHasOutputDuration,
		MempoolFindOutputsDuration,
		MempoolSize,

		TxSignDuration,
		TxVerifySigDuration,

		RedisGetDuration,
		RedisPipelineDuration,
	)
}

// ===============================
// HELPER
// ===============================
func ObserveDuration(h prometheus.Observer, start time.Time) {
	h.Observe(float64(time.Since(start).Milliseconds()))
}
