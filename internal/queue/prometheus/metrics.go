package prometheus

import (
	"github.com/bitmagnet-io/bitmagnet/internal/model"
	"github.com/prometheus/client_golang/prometheus"
)

// namespace used in fully-qualified metrics names.
const namespace = "bitmagnet_queue"

type QueueMetrics interface {
	Inc(queue string, status model.QueueJobStatus)
	Add(queue string, status model.QueueJobStatus, count float64)
}

type queueMetrics struct {
	jobsTotal *prometheus.CounterVec
}

func (m *queueMetrics) Inc(queue string, status model.QueueJobStatus) {
	m.jobsTotal.WithLabelValues(queue, string(status)).Inc()
}

func (m *queueMetrics) Add(queue string, status model.QueueJobStatus, count float64) {
	m.jobsTotal.WithLabelValues(queue, string(status)).Add(count)
}
