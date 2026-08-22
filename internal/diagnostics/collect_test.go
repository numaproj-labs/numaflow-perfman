package diagnostics_test

import (
	"context"
	"strings"
	"testing"

	"numa-perfman/internal/cluster"
	"numa-perfman/internal/diagnostics"
)

type fakeCluster struct {
	cluster.Client
	runner *recordingRunner
}

type recordingRunner struct {
	records [][]string
}

func (r *recordingRunner) Run(ctx context.Context, name string, args ...string) (string, string, error) {
	r.records = append(r.records, append([]string{name}, args...))
	switch {
	case contains(args, "pods"):
		return "pipeline-pod-1   1/1   Running\n", "", nil
	case contains(args, "events"):
		return strings.Repeat("event-line\n", 300), "", nil
	case contains(args, "logs"):
		return "log line\n", "", nil
	default:
		return "", "", nil
	}
}

func (r *recordingRunner) Start(ctx context.Context, name string, args ...string) (cluster.Process, error) {
	return nil, nil
}

func contains(args []string, s string) bool {
	for _, a := range args {
		if a == s {
			return true
		}
	}
	return false
}

func TestCollectBoundedEvents(t *testing.T) {
	r := &recordingRunner{}
	c := diagnostics.Collector{
		Cluster: cluster.Client{Runner: r},
		Opts:    diagnostics.Options{MaxEventLines: 10, LogTailLines: 5, MaxPodListRunes: 1024},
	}
	bundle, err := c.Collect(context.Background(), "run-ns")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(bundle.Events, "event-line") > 10 {
		t.Fatalf("events not truncated: %d lines", strings.Count(bundle.Events, "\n"))
	}
	if bundle.Pods == "" {
		t.Fatal("expected pods")
	}
}
