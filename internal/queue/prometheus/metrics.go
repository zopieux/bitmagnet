package prometheus

import (
	"time"

	"github.com/bitmagnet-io/bitmagnet/internal/model"
	"github.com/prometheus/client_golang/prometheus"
)

// namespace used in fully-qualified metrics names.
const namespace = "bitmagnet_queue"

type QueueMetrics interface {
	Inc(queue string, status model.QueueJobStatus)
	Add(queue string, status model.QueueJobStatus, count float64)
	JobStarted(queue string)
	JobFinished(queue string, status model.QueueJobStatus, waitDuration time.Duration, execDuration time.Duration)
}

type queueMetrics struct {
	jobsTotal       *prometheus.CounterVec
	jobsActive      *prometheus.GaugeVec
	jobWaitDuration *prometheus.HistogramVec
	jobDuration     *prometheus.HistogramVec
}

func (m *queueMetrics) Inc(queue string, status model.QueueJobStatus) {
	m.jobsTotal.WithLabelValues(queue, string(status)).Inc()
}

func (m *queueMetrics) Add(queue string, status model.QueueJobStatus, count float64) {
	m.jobsTotal.WithLabelValues(queue, string(status)).Add(count)
}

func (m *queueMetrics) JobStarted(queue string) {
	m.jobsActive.WithLabelValues(queue).Inc()
}

func (m *queueMetrics) JobFinished(queue string, status model.QueueJobStatus, waitDuration time.Duration, execDuration time.Duration) {
	m.jobsActive.WithLabelValues(queue).Dec()
	m.jobsTotal.WithLabelValues(queue, string(status)).Inc()
	m.jobWaitDuration.WithLabelValues(queue).Observe(waitDuration.Seconds())
	m.jobDuration.WithLabelValues(queue, string(status)).Observe(execDuration.Seconds())
}
