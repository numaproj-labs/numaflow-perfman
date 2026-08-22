package validation_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"numa-perfman/internal/cluster"
	"numa-perfman/internal/oracle"
	"numa-perfman/internal/validation"
)

type clusterRecordingRunner struct {
	calls []string
}

func (r *clusterRecordingRunner) Run(_ context.Context, name string, args ...string) (string, string, error) {
	r.calls = append(r.calls, name+":"+strings.Join(args, " "))
	return "", "", nil
}

func (r *clusterRecordingRunner) Start(context.Context, string, ...string) (cluster.Process, error) {
	return nil, nil
}

func TestClusterAdapterDeployControllerRejectsBareTag(t *testing.T) {
	t.Parallel()
	adapter := validation.ClusterAdapter{
		Cluster: cluster.Client{Runner: &clusterRecordingRunner{}},
	}
	err := adapter.DeployController(context.Background(), "numaflow-validation-deadbeef-map", "v1.2.3")
	if err == nil {
		t.Fatal("expected invalid image error")
	}
}

func TestClusterAdapterCreateNamespaceRequiresBoundRun(t *testing.T) {
	t.Parallel()
	adapter := validation.ClusterAdapter{
		Cluster: cluster.Client{Runner: &clusterRecordingRunner{}},
	}
	if err := adapter.CreateNamespace(context.Background(), "numaflow-validation-deadbeef-map"); err == nil {
		t.Fatal("expected bind error")
	}
}

func TestClusterAdapterDeployScenarioRequiresUDF(t *testing.T) {
	t.Parallel()
	adapter := validation.ClusterAdapter{
		Cluster: cluster.Client{Runner: &clusterRecordingRunner{}},
	}
	err := adapter.DeployScenario(context.Background(), "ns", oracle.ScenarioMap, "validation_db", "")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestClusterAdapterWaitScenarioReadyUsesRunningPhase(t *testing.T) {
	t.Parallel()
	runner := &clusterRecordingRunner{}
	adapter := validation.ClusterAdapter{
		Cluster:       cluster.Client{Runner: runner},
		DeployTimeout: time.Minute,
	}

	if err := adapter.WaitScenarioReady(context.Background(), "run-ns", oracle.ScenarioMap, 0); err != nil {
		t.Fatal(err)
	}
	if len(runner.calls) != 1 {
		t.Fatalf("calls = %v", runner.calls)
	}
	if !strings.Contains(runner.calls[0], "pipeline/map --for=jsonpath={.status.phase}=Running") {
		t.Fatalf("pipeline readiness call = %q", runner.calls[0])
	}
}

func TestClusterAdapterWaitMonoVertexReadyUsesRunningPhase(t *testing.T) {
	t.Parallel()
	runner := &clusterRecordingRunner{}
	adapter := validation.ClusterAdapter{
		Cluster:       cluster.Client{Runner: runner},
		DeployTimeout: time.Minute,
	}

	if err := adapter.WaitScenarioReady(context.Background(), "run-ns", oracle.ScenarioMonoVertex, 0); err != nil {
		t.Fatal(err)
	}
	if len(runner.calls) != 1 {
		t.Fatalf("calls = %v", runner.calls)
	}
	if !strings.Contains(runner.calls[0], "monovertex/monovertex --for=jsonpath={.status.phase}=Running") {
		t.Fatalf("MonoVertex readiness call = %q", runner.calls[0])
	}
}

func TestClusterAdapterDeleteScenarioWaitsForFinalizers(t *testing.T) {
	t.Parallel()
	runner := &clusterRecordingRunner{}
	adapter := validation.ClusterAdapter{
		Cluster:       cluster.Client{Runner: runner},
		DeployTimeout: time.Minute,
	}

	if err := adapter.DeleteScenario(context.Background(), "run-ns", oracle.ScenarioMap, 0); err != nil {
		t.Fatal(err)
	}
	if len(runner.calls) != 4 {
		t.Fatalf("calls = %v", runner.calls)
	}
	for i, want := range []string{
		"delete pipeline/map --ignore-not-found=true",
		"wait pipeline/map --for=delete",
		"delete interstepbufferservice/map --ignore-not-found=true",
		"wait interstepbufferservice/map --for=delete",
	} {
		if !strings.Contains(runner.calls[i], want) {
			t.Fatalf("calls[%d] = %q, want %q", i, runner.calls[i], want)
		}
	}
}
