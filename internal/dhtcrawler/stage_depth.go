package dhtcrawler

import (
	"sync"

	"github.com/prometheus/client_golang/prometheus"
)

// stageDepthCollector exposes the crawler pipeline's per-stage queue depth:
// the number of items waiting in each stage channel, read at scrape time.
// A stage whose depth keeps growing is the pipeline's bottleneck.
type stageDepthCollector struct {
	desc   *prometheus.Desc
	mu     sync.RWMutex
	probes map[string]func() int
}

func newStageDepthCollector() *stageDepthCollector {
	return &stageDepthCollector{
		desc: prometheus.NewDesc(
			"bitmagnet_dht_crawler_stage_queue_depth",
			"Number of items currently queued in each DHT crawler pipeline stage.",
			[]string{"stage"}, nil,
		),
		probes: map[string]func() int{},
	}
}

func (c *stageDepthCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.desc
}

func (c *stageDepthCollector) Collect(ch chan<- prometheus.Metric) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	for stage, probe := range c.probes {
		ch <- prometheus.MustNewConstMetric(c.desc, prometheus.GaugeValue, float64(probe()), stage)
	}
}

func (c *stageDepthCollector) setProbes(probes map[string]func() int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.probes = probes
}
