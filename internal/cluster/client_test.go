package cluster_test

import (
	"context"
	"reflect"
	"testing"

	"numa-perfman/internal/cluster"
)

type staticRunner struct {
	stdout string
}

func (r staticRunner) Run(context.Context, string, ...string) (string, string, error) {
	return r.stdout, "", nil
}

func (staticRunner) Start(context.Context, string, ...string) (cluster.Process, error) {
	return nil, nil
}

type recordingRunner struct {
	name string
	args []string
}

func (r *recordingRunner) Run(_ context.Context, name string, args ...string) (string, string, error) {
	r.name = name
	r.args = append([]string(nil), args...)
	return "", "", nil
}

func (*recordingRunner) Start(context.Context, string, ...string) (cluster.Process, error) {
	return nil, nil
}

func TestKubectlArgsIncludeContextAndNamespace(t *testing.T) {
	c := cluster.Client{Context: "kind-numaflow"}
	args := c.KubectlArgsForTest("run-ns", "get", "pods")
	want := []string{"--context", "kind-numaflow", "-n", "run-ns", "get", "pods"}
	if len(args) != len(want) {
		t.Fatalf("args len %d want %d: %v", len(args), len(want), args)
	}
	for i := range want {
		if args[i] != want[i] {
			t.Fatalf("args[%d]=%q want %q", i, args[i], want[i])
		}
	}
}

func TestDefaultStorageClassReadsMetadataAnnotations(t *testing.T) {
	c := cluster.Client{Runner: staticRunner{stdout: `{
		"items": [{
			"metadata": {
				"name": "standard",
				"annotations": {
					"storageclass.kubernetes.io/is-default-class": "true"
				}
			}
		}]
	}`}}

	got, err := c.DefaultStorageClass(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got != "standard" {
		t.Fatalf("default storage class = %q, want standard", got)
	}
}

func TestClearResourceFinalizersPatchesMetadata(t *testing.T) {
	runner := &recordingRunner{}
	c := cluster.Client{Context: "kind-numaflow", Runner: runner}

	if err := c.ClearResourceFinalizers(context.Background(), "run-ns", "pipeline/map", true); err != nil {
		t.Fatal(err)
	}
	if runner.name != "kubectl" {
		t.Fatalf("command = %q, want kubectl", runner.name)
	}
	want := []string{
		"--context", "kind-numaflow",
		"-n", "run-ns",
		"patch", "pipeline/map",
		"--type=merge",
		"-p", `{"metadata":{"finalizers":[]}}`,
	}
	if !reflect.DeepEqual(runner.args, want) {
		t.Fatalf("args = %v, want %v", runner.args, want)
	}
}

func TestKubectlArgsForTestNoNamespace(t *testing.T) {
	c := cluster.Client{Context: "ctx1"}
	args := c.KubectlArgsForTest("", "get", "ns")
	if len(args) < 3 || args[0] != "--context" || args[2] != "get" {
		t.Fatalf("args: %v", args)
	}
}
