package scenario_test

import (
	"strings"
	"testing"

	"numa-perfman/internal/central"
	"numa-perfman/internal/controller"
	"numa-perfman/internal/scenario"
)

func TestAllRenderBundlesPolicy(t *testing.T) {
	benchmarks := scenario.ListBenchmarks()
	for _, id := range benchmarks {
		in := scenario.RenderInput{
			ScenarioID: id, Kind: scenario.RunKindBenchmark,
			Namespace: testNS, UDFImage: testUDF,
		}
		bundle, err := scenario.RenderManifests(in)
		if err != nil {
			t.Fatalf("benchmark/%s: %v", id, err)
		}
		assertManifestPolicy(t, "benchmark/"+id, bundle.AllDocuments(), in)
	}
	for _, id := range scenario.ListValidations() {
		in := scenario.RenderInput{
			ScenarioID: id, Kind: scenario.RunKindValidation,
			Namespace: "numaflow-validation-deadbeef", UDFImage: testUDF,
			ValidationNS: validationNS, PostgresDBName: testDB,
		}
		bundle, err := scenario.RenderManifests(in)
		if err != nil {
			t.Fatalf("validation/%s: %v", id, err)
		}
		assertManifestPolicy(t, "validation/"+id, bundle.AllDocuments(), in)
	}

	server, err := central.RenderServer(central.ServerRenderInput{
		Namespace: testCentralNS, MonitoringNamespace: testMonitoringNS, ImageReference: testUIImage,
	})
	if err != nil {
		t.Fatal(err)
	}
	all := server.Concat()
	if err := central.AssertBundlePolicy("server", all, testUIImage); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(all, testCentralNS) {
		t.Fatal("server missing namespace")
	}

	prom, err := central.RenderPrometheus(central.PrometheusRenderInput{Namespace: testMonitoringNS})
	if err != nil {
		t.Fatal(err)
	}
	promAll := prom.Concat()
	if err := central.AssertBundlePolicy("prometheus", promAll, ""); err != nil {
		t.Fatal(err)
	}
	for _, frag := range []string{"relabel_configs", "prometheus-data", "target_label: run_id", "pg_isready"} {
		if frag == "pg_isready" {
			continue
		}
		if !strings.Contains(promAll, frag) {
			t.Fatalf("prometheus missing %q", frag)
		}
	}

	pg, err := central.RenderValidationPostgres(central.PostgresRenderInput{Namespace: validationNS, StorageClassName: "standard"})
	if err != nil {
		t.Fatal(err)
	}
	pgAll := pg.Concat()
	if err := central.AssertBundlePolicy("postgres", pgAll, ""); err != nil {
		t.Fatal(err)
	}
	for _, frag := range []string{central.PostgresSecretName, "pg_isready", "volumeClaimTemplates", testValidationNS} {
		if !strings.Contains(pgAll, frag) {
			t.Fatalf("postgres missing %q", frag)
		}
	}

	ctrl, err := controller.Render(controller.RenderInput{Namespace: testNS, ImageReference: testImage})
	if err != nil {
		t.Fatal(err)
	}
	ctrlAll := ctrl.Concat()
	if strings.Contains(ctrlAll, "gp3") {
		t.Fatal("controller must not reference forbidden storage")
	}
	if !strings.Contains(ctrlAll, testImage) || !strings.Contains(ctrlAll, controller.ImagePullPolicy) {
		t.Fatal("controller image/policy mismatch")
	}
}

func assertManifestPolicy(t *testing.T, name string, docs []string, in scenario.RenderInput) {
	t.Helper()
	if err := scenario.ValidateManifestContent(docs); err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	for _, doc := range docs {
		if !strings.Contains(doc, in.Namespace) {
			t.Fatalf("%s: missing namespace", name)
		}
	}
	if in.Kind == scenario.RunKindValidation {
		joined := strings.Join(docs, "\n")
		dsn := central.PostgresServiceName + "." + validationNS + ".svc"
		if !strings.Contains(joined, dsn) || !strings.Contains(joined, in.PostgresDBName) {
			t.Fatalf("%s: validation DSN/db", name)
		}
		if !strings.Contains(joined, central.PostgresSecretName) {
			t.Fatalf("%s: postgres secret", name)
		}
	}
}

const (
	testCentralNS    = "numaflow-system"
	testMonitoringNS = "monitoring"
	testValidationNS = "validation-system"
	testUIImage      = "quay.io/numaproj/numaflow:v1.8.0"
)
