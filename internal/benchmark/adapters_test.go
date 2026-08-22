package benchmark_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"numa-perfman/internal/benchmark"
	"numa-perfman/internal/cluster"
	"numa-perfman/internal/scenario"
)

type benchmarkRecordingRunner struct {
	calls []string
}

func (r *benchmarkRecordingRunner) Run(_ context.Context, name string, args ...string) (string, string, error) {
	r.calls = append(r.calls, name+":"+strings.Join(args, " "))
	return "", "", nil
}

func (r *benchmarkRecordingRunner) Start(context.Context, string, ...string) (cluster.Process, error) {
	return nil, nil
}

func TestClusterAdapterWaitScenarioReadyUsesRunningPhase(t *testing.T) {
	t.Parallel()
	runner := &benchmarkRecordingRunner{}
	adapter := benchmark.ClusterAdapter{
		Cluster:       cluster.Client{Runner: runner},
		DeployTimeout: time.Minute,
	}
	sc, err := scenario.LookupBenchmark("single-map")
	if err != nil {
		t.Fatal(err)
	}

	if err := adapter.WaitScenarioReady(context.Background(), "run-ns", sc, 0); err != nil {
		t.Fatal(err)
	}
	if len(runner.calls) != 1 {
		t.Fatalf("calls = %v", runner.calls)
	}
	if !strings.Contains(runner.calls[0], "pipeline/single-map --for=jsonpath={.status.phase}=Running") {
		t.Fatalf("pipeline readiness call = %q", runner.calls[0])
	}
}

func TestClusterAdapterWaitMonoVertexReadyUsesRunningPhase(t *testing.T) {
	t.Parallel()
	runner := &benchmarkRecordingRunner{}
	adapter := benchmark.ClusterAdapter{
		Cluster:       cluster.Client{Runner: runner},
		DeployTimeout: time.Minute,
	}
	sc, err := scenario.LookupBenchmark("monovertex-generator-blackhole")
	if err != nil {
		t.Fatal(err)
	}

	if err := adapter.WaitScenarioReady(context.Background(), "run-ns", sc, 0); err != nil {
		t.Fatal(err)
	}
	if len(runner.calls) != 1 {
		t.Fatalf("calls = %v", runner.calls)
	}
	if !strings.Contains(runner.calls[0], "monovertex/monovertex-generator-blackhole --for=jsonpath={.status.phase}=Running") {
		t.Fatalf("MonoVertex readiness call = %q", runner.calls[0])
	}
}

func TestClusterAdapterDeleteScenarioWaitsForFinalizers(t *testing.T) {
	t.Parallel()
	runner := &benchmarkRecordingRunner{}
	adapter := benchmark.ClusterAdapter{
		Cluster:       cluster.Client{Runner: runner},
		DeployTimeout: time.Minute,
	}
	sc, err := scenario.LookupBenchmark("single-map")
	if err != nil {
		t.Fatal(err)
	}

	if err := adapter.DeleteScenario(context.Background(), "run-ns", sc, 0); err != nil {
		t.Fatal(err)
	}
	if len(runner.calls) != 4 {
		t.Fatalf("calls = %v", runner.calls)
	}
	for i, want := range []string{
		"delete pipeline/single-map --ignore-not-found=true",
		"wait pipeline/single-map --for=delete",
		"delete interstepbufferservice/single-map --ignore-not-found=true",
		"wait interstepbufferservice/single-map --for=delete",
	} {
		if !strings.Contains(runner.calls[i], want) {
			t.Fatalf("calls[%d] = %q, want %q", i, runner.calls[i], want)
		}
	}
}
