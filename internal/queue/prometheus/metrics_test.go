package prometheus

import (
	"testing"

	"github.com/bitmagnet-io/bitmagnet/internal/model"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/require"
)

func TestQueueMetricsCollector(t *testing.T) {
	res := New()
	reg := prometheus.NewRegistry()
	require.NoError(t, reg.Register(res.Collector))

	res.Metrics.Inc("test_queue", model.QueueJobStatusPending)
	res.Metrics.Add("test_queue", model.QueueJobStatusProcessed, 3)
	res.Metrics.Inc("test_queue", model.QueueJobStatusRetry)
	res.Metrics.Inc("test_queue", model.QueueJobStatusFailed)

	metricFamilies, err := reg.Gather()
	require.NoError(t, err)
	require.Len(t, metricFamilies, 1)

	mf := metricFamilies[0]
	require.Equal(t, "bitmagnet_queue_jobs_total", *mf.Name)
	require.Equal(t, dto.MetricType_COUNTER, *mf.Type)

	statusCounts := make(map[string]float64)
	for _, m := range mf.Metric {
		var status string
		for _, label := range m.Label {
			if *label.Name == "status" {
				status = *label.Value
			}
		}
		statusCounts[status] = *m.Counter.Value
	}

	require.Equal(t, float64(1), statusCounts[string(model.QueueJobStatusPending)])
	require.Equal(t, float64(3), statusCounts[string(model.QueueJobStatusProcessed)])
	require.Equal(t, float64(1), statusCounts[string(model.QueueJobStatusRetry)])
	require.Equal(t, float64(1), statusCounts[string(model.QueueJobStatusFailed)])
}
