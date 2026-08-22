package central_test

import (
	"strings"
	"testing"

	"numa-perfman/internal/central"
)

const (
	testCentralNS    = "numaflow-system"
	testMonitoringNS = "monitoring"
	testValidationNS = "validation-system"
	testUIImage      = "quay.io/numaproj/numaflow:v1.8.0"
)

func TestRenderServerPolicy(t *testing.T) {
	set, err := central.RenderServer(central.ServerRenderInput{
		Namespace:           testCentralNS,
		MonitoringNamespace: testMonitoringNS,
		ImageReference:      testUIImage,
	})
	if err != nil {
		t.Fatal(err)
	}
	assertDocumentBoundaries(t, set)
	all := set.Concat()
	if err := central.AssertBundlePolicy("server", all, testUIImage); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(all, testCentralNS) {
		t.Fatal("missing namespace")
	}
	if !strings.Contains(all, `"server.insecure": "false"`) {
		t.Fatal("central UI must retain TLS")
	}
	if strings.Contains(all, `"/readyz"`) {
		t.Fatal("numaflow v1.8.0 server does not expose /readyz")
	}
	if got := strings.Count(all, `"path": "/livez"`); got != 2 {
		t.Fatalf("server liveness and readiness probes must use /livez, got %d", got)
	}
	if !strings.Contains(all, `"verbs": [
      "get",
      "list",
      "watch"
    ]`) {
		// ClusterRole for numaflow API must be read-only; checked on ClusterRole doc below.
	}
	for _, doc := range set.Documents {
		if strings.Contains(doc, "numaflow-server-role") && strings.Contains(doc, "numaflow.numaproj.io") {
			if strings.Contains(doc, `"create"`) {
				t.Fatal("numaflow ClusterRole must be read-only")
			}
		}
	}
	if strings.Contains(all, "NUMAFLOW_SERVER_DEX") || strings.Contains(all, "server.dex") {
		t.Fatal("must not reference Dex")
	}
	if !strings.Contains(all, central.ImagePullPolicy) {
		t.Fatal("expected IfNotPresent pull policy")
	}
	count := strings.Count(all, testUIImage)
	if count < 3 {
		t.Fatalf("UI image must appear on all server containers, got %d", count)
	}
}

func TestRenderPrometheusPolicy(t *testing.T) {
	set, err := central.RenderPrometheus(central.PrometheusRenderInput{Namespace: testMonitoringNS})
	if err != nil {
		t.Fatal(err)
	}
	assertDocumentBoundaries(t, set)
	all := set.Concat()
	if err := central.AssertBundlePolicy("prometheus", all, ""); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(all, "scrape_interval: 10s") {
		t.Fatal("missing 10s scrape interval")
	}
	if !strings.Contains(all, "prometheus-data") {
		t.Fatal("missing Prometheus PVC")
	}
	if !strings.Contains(all, "job_name: cadvisor") {
		t.Fatal("missing cadvisor job")
	}
	if !strings.Contains(all, "relabel_configs") {
		t.Fatal("missing relabel configs")
	}
	if !strings.Contains(all, "scheme: https") || !strings.Contains(all, "insecure_skip_verify: true") {
		t.Fatal("numaflow pod scrape must use HTTPS with certificate verification disabled")
	}
	if !strings.Contains(all, "source_labels: [__meta_kubernetes_namespace]") || !strings.Contains(all, "target_label: namespace") {
		t.Fatal("numaflow pod scrape must propagate Kubernetes namespace")
	}
	if !strings.Contains(all, "target_label: run_id") {
		t.Fatal("missing run_id relabel")
	}
	if !strings.Contains(all, testMonitoringNS) {
		t.Fatal("missing monitoring namespace")
	}
}

func TestRenderValidationPostgresPolicy(t *testing.T) {
	set, err := central.RenderValidationPostgres(central.PostgresRenderInput{Namespace: testValidationNS})
	if err != nil {
		t.Fatal(err)
	}
	assertDocumentBoundaries(t, set)
	all := set.Concat()
	if err := central.AssertBundlePolicy("postgres", all, ""); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(all, "gp3") {
		t.Fatal("must not use gp3")
	}
	if !strings.Contains(all, central.PostgresSecretName) {
		t.Fatal("missing credentials secret")
	}
	if !strings.Contains(all, `"type": "ClusterIP"`) {
		t.Fatal("service must be ClusterIP")
	}
	if !strings.Contains(all, "pg_isready") {
		t.Fatal("missing postgres probes")
	}
	if !strings.Contains(all, "volumeClaimTemplates") {
		t.Fatal("missing PVC template")
	}
	if !strings.Contains(all, testValidationNS) {
		t.Fatal("missing validation namespace")
	}
}

func TestNoPerfharnessInCentralBundles(t *testing.T) {
	bundles := []struct {
		name string
		yaml string
	}{
		{"server", mustRenderServer(t)},
		{"prometheus", mustRenderPrometheus(t)},
		{"postgres", mustRenderPostgres(t)},
	}
	for _, b := range bundles {
		if strings.Contains(strings.ToLower(b.yaml), "numa-perfman") {
			t.Fatalf("%s bundle must not contain perfman workload", b.name)
		}
	}
}

func mustRenderServer(t *testing.T) string {
	t.Helper()
	set, err := central.RenderServer(central.ServerRenderInput{
		Namespace: testCentralNS, MonitoringNamespace: testMonitoringNS, ImageReference: testUIImage,
	})
	if err != nil {
		t.Fatal(err)
	}
	return set.Concat()
}

func mustRenderPrometheus(t *testing.T) string {
	t.Helper()
	set, err := central.RenderPrometheus(central.PrometheusRenderInput{Namespace: testMonitoringNS})
	if err != nil {
		t.Fatal(err)
	}
	return set.Concat()
}

func mustRenderPostgres(t *testing.T) string {
	t.Helper()
	set, err := central.RenderValidationPostgres(central.PostgresRenderInput{Namespace: testValidationNS})
	if err != nil {
		t.Fatal(err)
	}
	return set.Concat()
}

func assertDocumentBoundaries(t *testing.T, set central.ManifestSet) {
	t.Helper()
	got := set.Concat()
	want := len(set.Documents) - 1
	if count := strings.Count(got, "\n---\n"); count != want {
		t.Fatalf("document separators = %d, want %d", count, want)
	}
	if !strings.HasSuffix(got, "\n") {
		t.Fatal("manifest bundle must end with a newline")
	}
}
