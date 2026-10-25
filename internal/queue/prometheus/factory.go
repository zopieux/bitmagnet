package prometheus

import (
	"github.com/prometheus/client_golang/prometheus"
	"go.uber.org/fx"
)

type Result struct {
	fx.Out
	JobsTotal       prometheus.Collector `group:"prometheus_collectors"`
	JobsActive      prometheus.Collector `group:"prometheus_collectors"`
	JobWaitDuration prometheus.Collector `group:"prometheus_collectors"`
	JobDuration     prometheus.Collector `group:"prometheus_collectors"`
	Metrics         QueueMetrics
}

func New() Result {
	jobsTotal := prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "jobs_total",
		Help:      "Number of queue jobs; broken down by queue and status.",
	}, []string{"queue", "status"})

	jobsActive := prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: namespace,
		Name:      "jobs_active",
		Help:      "Number of queue jobs currently executing; broken down by queue.",
	}, []string{"queue"})

	jobWaitDuration := prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: namespace,
		Name:      "job_wait_duration_seconds",
		Help:      "Queue job wait latency in seconds between scheduled time and execution start; broken down by queue.",
		Buckets:   []float64{0.01, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60, 120, 300},
	}, []string{"queue"})

	jobDuration := prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: namespace,
		Name:      "job_duration_seconds",
		Help:      "Queue job execution duration in seconds; broken down by queue and status.",
		Buckets:   []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60},
	}, []string{"queue", "status"})

	return Result{
		JobsTotal:       jobsTotal,
		JobsActive:      jobsActive,
		JobWaitDuration: jobWaitDuration,
		JobDuration:     jobDuration,
		Metrics: &queueMetrics{
			jobsTotal:       jobsTotal,
			jobsActive:      jobsActive,
			jobWaitDuration: jobWaitDuration,
			jobDuration:     jobDuration,
		},
	}
}
