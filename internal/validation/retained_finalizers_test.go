package validation

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"numa-perfman/internal/cluster"
)

type finalizerRecordingRunner struct {
	calls []string
}

func (r *finalizerRecordingRunner) Run(_ context.Context, name string, args ...string) (string, string, error) {
	r.calls = append(r.calls, name+":"+strings.Join(args, " "))
	return "", "", nil
}

func (*finalizerRecordingRunner) Start(context.Context, string, ...string) (cluster.Process, error) {
	return nil, nil
}

func TestClearRetainedScenarioFinalizersForPipelineValidation(t *testing.T) {
	t.Parallel()
	runner := &finalizerRecordingRunner{}
	adapter := ClusterAdapter{
		Cluster: cluster.Client{Runner: runner},
	}
	adapter.BindRunID(uuid.New(), "quay.io/numaproj/numaflow:v1.8.2", "map")

	if err := adapter.clearRetainedScenarioFinalizers(context.Background(), "run-ns"); err != nil {
		t.Fatal(err)
	}
	if len(runner.calls) != 2 {
		t.Fatalf("calls = %v", runner.calls)
	}
	for i, want := range []string{
		"patch pipeline/map --type=merge -p {\"metadata\":{\"finalizers\":[]}}",
		"patch interstepbufferservice/map --type=merge -p {\"metadata\":{\"finalizers\":[]}}",
	} {
		if !strings.Contains(runner.calls[i], want) {
			t.Fatalf("calls[%d] = %q, want %q", i, runner.calls[i], want)
		}
	}
}

func TestClearRetainedScenarioFinalizersForMonoVertexValidation(t *testing.T) {
	t.Parallel()
	runner := &finalizerRecordingRunner{}
	adapter := ClusterAdapter{
		Cluster: cluster.Client{Runner: runner},
	}
	adapter.BindRunID(uuid.New(), "quay.io/numaproj/numaflow:v1.8.2", "monovertex")

	if err := adapter.clearRetainedScenarioFinalizers(context.Background(), "run-ns"); err != nil {
		t.Fatal(err)
	}
	if len(runner.calls) != 1 {
		t.Fatalf("calls = %v", runner.calls)
	}
	if !strings.Contains(runner.calls[0], "patch monovertex/monovertex --type=merge -p {\"metadata\":{\"finalizers\":[]}}") {
		t.Fatalf("call = %q", runner.calls[0])
	}
}
