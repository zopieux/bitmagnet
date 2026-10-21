package prometheus

import (
	"github.com/prometheus/client_golang/prometheus"
	"go.uber.org/fx"
)

type Result struct {
	fx.Out
	Collector prometheus.Collector `group:"prometheus_collectors"`
	Metrics   QueueMetrics
}

func New() Result {
	jobsTotal := prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "jobs_total",
		Help:      "Number of queue jobs; broken down by queue and status.",
	}, []string{"queue", "status"})

	return Result{
		Collector: jobsTotal,
		Metrics: &queueMetrics{
			jobsTotal: jobsTotal,
		},
	}
}
