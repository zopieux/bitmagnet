package prometheus

import (
	"testing"
	"time"

	"github.com/bitmagnet-io/bitmagnet/internal/model"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/require"
)

func TestQueueMetricsCollector(t *testing.T) {
	res := New()
	reg := prometheus.NewRegistry()
	require.NoError(t, reg.Register(res.JobsTotal))
	require.NoError(t, reg.Register(res.JobsActive))
	require.NoError(t, reg.Register(res.JobWaitDuration))
	require.NoError(t, reg.Register(res.JobDuration))

	res.Metrics.Inc("test_queue", model.QueueJobStatusPending)
	res.Metrics.Add("test_queue", model.QueueJobStatusProcessed, 3)
	res.Metrics.Inc("test_queue", model.QueueJobStatusRetry)
	res.Metrics.Inc("test_queue", model.QueueJobStatusFailed)

	res.Metrics.JobStarted("test_queue")
	res.Metrics.JobFinished("test_queue", model.QueueJobStatusProcessed, 250*time.Millisecond, 100*time.Millisecond)

	metricFamilies, err := reg.Gather()
	require.NoError(t, err)

	byName := make(map[string]*dto.MetricFamily)
	for _, mf := range metricFamilies {
		byName[*mf.Name] = mf
	}

	require.Contains(t, byName, "bitmagnet_queue_jobs_total")
	require.Contains(t, byName, "bitmagnet_queue_jobs_active")
	require.Contains(t, byName, "bitmagnet_queue_job_wait_duration_seconds")
	require.Contains(t, byName, "bitmagnet_queue_job_duration_seconds")

	mfTotal := byName["bitmagnet_queue_jobs_total"]
	require.Equal(t, dto.MetricType_COUNTER, *mfTotal.Type)

	statusCounts := make(map[string]float64)
	for _, m := range mfTotal.Metric {
		var status string
		for _, label := range m.Label {
			if *label.Name == "status" {
				status = *label.Value
			}
		}
		statusCounts[status] = *m.Counter.Value
	}

	require.Equal(t, float64(1), statusCounts[string(model.QueueJobStatusPending)])
	require.Equal(t, float64(4), statusCounts[string(model.QueueJobStatusProcessed)])
	require.Equal(t, float64(1), statusCounts[string(model.QueueJobStatusRetry)])
	require.Equal(t, float64(1), statusCounts[string(model.QueueJobStatusFailed)])

	mfActive := byName["bitmagnet_queue_jobs_active"]
	require.Equal(t, dto.MetricType_GAUGE, *mfActive.Type)
	require.Equal(t, float64(0), *mfActive.Metric[0].Gauge.Value)
}
