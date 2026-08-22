package setup_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"numa-perfman/internal/cluster"
	"numa-perfman/internal/config"
	"numa-perfman/internal/setup"
)

type crdRecordingCluster struct {
	fakeCluster
	appliedYAML string
}

func (c *crdRecordingCluster) ApplyCRDsFromPath(ctx context.Context, path string, strict bool) error {
	docs, err := cluster.LoadYAMLDocumentsFromPath(path)
	if err != nil {
		return err
	}
	filtered, err := cluster.FilterCRDDocuments(docs, strict)
	if err != nil {
		return err
	}
	c.appliedYAML = string(cluster.ConcatYAMLDocuments(filtered))
	return nil
}

func TestSetupApplyCRDsOnlySkipsController(t *testing.T) {
	dir := t.TempDir()
	deployDir := filepath.Join(dir, "deploy")
	if err := os.MkdirAll(deployDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeMetricsServerManifest(t, dir)
	mixed := filepath.Join(deployDir, "numaflow.yaml")
	content := `apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: pipelines.numaflow.numaproj.io
spec:
  group: numaflow.numaproj.io
  names:
    kind: Pipeline
    plural: pipelines
  scope: Namespaced
  versions: []
---
apiVersion: v1
kind: ServiceAccount
metadata:
  name: numaflow-sa
`
	if err := os.WriteFile(mixed, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	rec := &crdRecordingCluster{fakeCluster: fakeCluster{t: t}}
	cfg := config.Defaults()
	cfg.Image = "quay.io/numaproj/numaflow:v1.8.0"
	cfg.UDFImage = "numaflow-perfman-udfs:deadbeef"
	_, err := setup.Run(context.Background(), setup.Options{
		Config:       cfg,
		RepoRoot:     dir,
		SkipUDFBuild: true,
		Cluster:      rec,
		Runner:       fakeRunner{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(rec.appliedYAML, "ServiceAccount") {
		t.Fatal("setup must not apply non-CRD documents from default bundle path filtering")
	}
	if !strings.Contains(rec.appliedYAML, "CustomResourceDefinition") {
		t.Fatal("expected CRD document")
	}
}

func TestSetupStrictCRDsRejectsMixed(t *testing.T) {
	dir := t.TempDir()
	writeMetricsServerManifest(t, dir)
	mixed := filepath.Join(dir, "crds.yaml")
	if err := os.WriteFile(mixed, []byte(`apiVersion: v1
kind: Namespace
metadata:
  name: x
`), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := config.Defaults()
	cfg.Image = "quay.io/numaproj/numaflow:v1.8.0"
	cfg.UDFImage = "numaflow-perfman-udfs:deadbeef"
	_, err := setup.Run(context.Background(), setup.Options{
		Config:       cfg,
		RepoRoot:     dir,
		CRDsPath:     mixed,
		SkipUDFBuild: true,
		Cluster:      &fakeCluster{t: t},
		Runner:       fakeRunner{},
	})
	if err == nil || !strings.Contains(err.Error(), "reject non-CRD") {
		t.Fatalf("expected strict crd error, got %v", err)
	}
}
