//go:build integration

package integration

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"numa-perfman/internal/central"
	"numa-perfman/internal/config"
	"numa-perfman/internal/scenario"
)

// §16.3 (2): central Numaflow server RBAC can list pipeline resources in a temporary run namespace.
func TestCentralUICrossNamespaceListing(t *testing.T) {
	e := shared(t)

	runNS := "numaflow-perf-ui163"
	t.Cleanup(func() {
		_, _ = e.kubectl("delete", "namespace", runNS, "--ignore-not-found")
	})

	if _, err := e.kubectl("create", "namespace", runNS); err != nil {
		t.Fatal(err)
	}
	if _, err := e.kubectl("label", "namespace", runNS,
		"perfman.numaproj.io/managed-by=numaflow-perfman",
		"perfman.numaproj.io/run-id=00000000-0000-4000-8000-000000000001",
		"perfman.numaproj.io/run-kind=benchmark",
	); err != nil {
		t.Fatal(err)
	}

	bundle, err := scenario.RenderManifests(scenario.RenderInput{
		ScenarioID: "single-map",
		Kind:       scenario.RunKindBenchmark,
		Namespace:  runNS,
		UDFImage:   readUDFImage(t, e),
		RunID:      "00000000-0000-4000-8000-000000000001",
	})
	if err != nil {
		t.Fatal(err)
	}

	manifestPath := writeConcatManifest(t, bundle.AllDocuments())
	if _, err := e.kubectl("apply", "-f", manifestPath); err != nil {
		t.Fatal(err)
	}

	ok, detail, err := e.serverCanListPipelinesIn(runNS)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatalf("central server SA cannot list pipelines in %s (central ns %q): %q",
			runNS, config.Defaults().CentralNamespace, detail)
	}

	sa := "system:serviceaccount:" + config.Defaults().CentralNamespace + ":" + central.ServerServiceAccountName
	listOut, err := e.kubectl("get", "pipelines.numaflow.numaproj.io", "-n", runNS, "--as="+sa)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(listOut, "single-map") {
		t.Fatalf("expected pipeline listed for central UI discovery, got:\n%s", listOut)
	}
}

func readUDFImage(t *testing.T, e *env) string {
	t.Helper()
	ctx := context.Background()
	repo, err := e.openResults(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	cfg, found, err := repo.LoadHarnessConfig(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !found || cfg.UDFImage == "" {
		t.Fatal("udf image missing from harness_config after setup")
	}
	return cfg.UDFImage
}

func writeConcatManifest(t *testing.T, docs []string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "manifest.yaml")
	body := strings.Join(docs, "---\n")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		t.Fatal(err)
	}
	return abs
}
