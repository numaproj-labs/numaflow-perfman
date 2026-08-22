package benchmark_test

import (
	"testing"
	"time"

	"numa-perfman/internal/benchmark"
	prom "numa-perfman/internal/prometheus"
	"numa-perfman/internal/scenario"
)

func TestValidateCollectedMetrics(t *testing.T) {
	t.Parallel()
	defs := []scenario.MetricDefinition{
		{Name: "a", Required: true, MinSamples: 2},
	}
	now := time.Unix(0, 0)
	ok := []prom.SeriesResult{{Metric: defs[0], Samples: []prom.Sample{{Timestamp: now, Value: 1}, {Timestamp: now, Value: 2}}}}
	if err := benchmark.ValidateCollectedMetrics(defs, ok); err != nil {
		t.Fatal(err)
	}
	bad := []prom.SeriesResult{{Metric: defs[0], Samples: []prom.Sample{{Timestamp: now, Value: 1}}}}
	if err := benchmark.ValidateCollectedMetrics(defs, bad); !benchmark.IsMetricsIncomplete(err) {
		t.Fatalf("got %v", err)
	}
}

func TestValidateCollectedGroupedMetrics(t *testing.T) {
	t.Parallel()
	defs := []scenario.MetricDefinition{
		{Name: "forwarder_rate", Required: true, MinSamples: 2, GroupBy: []string{"vertex"}},
	}
	now := time.Unix(0, 0)
	ok := []prom.SeriesResult{
		{Metric: defs[0], Labels: map[string]string{"vertex": "in"}, Samples: []prom.Sample{{Timestamp: now, Value: 1}, {Timestamp: now, Value: 2}}},
		{Metric: defs[0], Labels: map[string]string{"vertex": "map"}, Samples: []prom.Sample{{Timestamp: now, Value: 1}}},
	}
	if err := benchmark.ValidateCollectedMetrics(defs, ok); err != nil {
		t.Fatal(err)
	}
}
