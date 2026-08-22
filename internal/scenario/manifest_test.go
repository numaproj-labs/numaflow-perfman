package scenario_test

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
	"numa-perfman/internal/central"
	"numa-perfman/internal/controller"
	"numa-perfman/internal/scenario"
)

const (
	testNS       = "numaflow-perf-deadbeef"
	testUDF      = "numaflow-perfman-udfs:abc123"
	testImage    = "quay.io/numaproj/numaflow:v1.8.0"
	testRunID    = "12345678-1234-1234-1234-123456789abc"
	validationNS = "validation-system"
	testDB       = "validation_run_deadbeef"
)

func TestManifestAssertionsPlan162(t *testing.T) {
	benchmarks := scenario.ListBenchmarks()
	for _, id := range benchmarks {
		t.Run("benchmark/"+id, func(t *testing.T) {
			assertScenarioManifest(t, scenario.RenderInput{
				ScenarioID:    id,
				Kind:          scenario.RunKindBenchmark,
				Namespace:     testNS,
				UDFImage:      testUDF,
				RunID:         testRunID,
				NumaflowImage: testImage,
			})
			assertBenchmarkContent(t, id, scenario.RenderInput{
				ScenarioID:    id,
				Kind:          scenario.RunKindBenchmark,
				Namespace:     testNS,
				UDFImage:      testUDF,
				RunID:         testRunID,
				NumaflowImage: testImage,
			})
		})
	}
	validations := scenario.ListValidations()
	for _, id := range validations {
		t.Run("validation/"+id, func(t *testing.T) {
			in := scenario.RenderInput{
				ScenarioID:     id,
				Kind:           scenario.RunKindValidation,
				Namespace:      "numaflow-validation-deadbeef",
				UDFImage:       testUDF,
				ValidationNS:   validationNS,
				PostgresDBName: testDB,
				RunID:          testRunID,
				NumaflowImage:  testImage,
			}
			assertScenarioManifest(t, in)
			assertValidationContent(t, id, in)
		})
	}
}

func assertScenarioManifest(t *testing.T, in scenario.RenderInput) {
	t.Helper()
	bundle, err := scenario.RenderManifests(in)
	if err != nil {
		t.Fatal(err)
	}
	if bundle.ISB != "" {
		for _, fragment := range []string{
			"persistence:",
			"accessMode: ReadWriteOnce",
			"storageClassName: standard",
			"volumeSize: 1Gi",
		} {
			if !strings.Contains(bundle.ISB, fragment) {
				t.Fatalf("ISB manifest missing PVC persistence setting %q", fragment)
			}
		}
		if strings.Contains(bundle.ISB, "requests:\n          storage:") {
			t.Fatal("ISB manifest must not use storage as a container resource request")
		}
	}
	for _, doc := range bundle.AllDocuments() {
		var parsed any
		if err := yaml.Unmarshal([]byte(doc), &parsed); err != nil {
			t.Logf("invalid manifest:\n%s", doc)
			t.Fatalf("rendered manifest is invalid YAML: %v", err)
		}
		if !strings.Contains(doc, in.Namespace) {
			t.Fatalf("missing explicit namespace in manifest")
		}
		if strings.Contains(doc, testUDF) && !strings.Contains(doc, "imagePullPolicy: Never") {
			t.Fatal("udf must use Never pull policy")
		}
		if !strings.Contains(doc, "perfman.numaproj.io/scenario") {
			t.Fatal("missing scenario label")
		}
		if strings.Contains(doc, `perfman.numaproj.io/numaflow-image: "quay.io/`) {
			t.Fatal("complete image reference is not a valid Kubernetes label value")
		}
		if !strings.Contains(doc, `perfman.numaproj.io/numaflow-image-ref: "`+testImage+`"`) {
			t.Fatal("full image reference must be retained as an annotation")
		}
		if strings.Contains(doc, "kind: Pipeline\n") &&
			!strings.Contains(doc, "      metadata:\n        labels:") {
			t.Fatal("pipeline vertices must carry run labels for pod metric discovery")
		}
		if strings.Contains(doc, "kind: MonoVertex\n") &&
			!strings.Contains(doc, "spec:\n  metadata:\n    labels:") {
			t.Fatal("monovertex spec must carry run labels for pod metric discovery")
		}
	}
	ctrl, err := controller.Render(controller.RenderInput{Namespace: in.Namespace, ImageReference: testImage})
	if err != nil {
		t.Fatal(err)
	}
	dep := ctrl.Deployment
	if !strings.Contains(dep, testImage) {
		t.Fatal("controller image must equal requested reference")
	}
	if !strings.Contains(dep, `"NUMAFLOW_IMAGE"`) || !strings.Contains(dep, testImage) {
		t.Fatal("NUMAFLOW_IMAGE must equal requested reference")
	}
	if !strings.Contains(dep, controller.ImagePullPolicy) {
		t.Fatal("controller must use IfNotPresent")
	}
	if in.Kind == scenario.RunKindValidation {
		dsn := central.PostgresServiceName + "." + validationNS + ".svc"
		joined := strings.Join(bundle.AllDocuments(), "\n")
		if !strings.Contains(joined, dsn) || !strings.Contains(joined, testDB) {
			t.Fatal("validation manifests must use central postgres service and unique database")
		}
		if !strings.Contains(joined, central.PostgresSecretName) {
			t.Fatal("validation manifests must reference postgres credentials secret")
		}
	}
}

func assertBenchmarkContent(t *testing.T, id string, in scenario.RenderInput) {
	t.Helper()
	bundle, err := scenario.RenderManifests(in)
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(bundle.AllDocuments(), "\n")
	if strings.Contains(joined, "kind: MonoVertex") {
		if !strings.Contains(joined, "\nspec:\n") {
			t.Fatal("monovertex manifest must contain a spec")
		}
		if !strings.Contains(joined, "spec:\n  metadata:\n    labels:") {
			t.Fatal("monovertex workload metadata must carry run labels")
		}
	}
	switch id {
	case "single-map", "two-maps":
		if !strings.Contains(joined, "--benchmark-map") {
			t.Fatal("pipeline benchmark must use --benchmark-map")
		}
		if !strings.Contains(joined, "blackhole: {}") {
			t.Fatal("pipeline benchmark must sink to blackhole")
		}
	case "monovertex-generator-blackhole":
		if strings.Contains(joined, testUDF) {
			t.Fatal("generator-blackhole benchmark should not require UDF containers")
		}
	case "simple-monovertex":
		if !strings.Contains(joined, "--benchmark-transformer") || !strings.Contains(joined, "--benchmark-blackhole-sink") {
			t.Fatal("simple monovertex benchmark needs transformer and blackhole sink modes")
		}
	case "monovertex-map":
		if !strings.Contains(joined, "--benchmark-map") || !strings.Contains(joined, "--benchmark-blackhole-sink") {
			t.Fatal("monovertex-map benchmark needs map and sink modes")
		}
	case "monovertex-batchmap":
		if !strings.Contains(joined, "--benchmark-batchmap") {
			t.Fatal("monovertex-batchmap must use --benchmark-batchmap")
		}
	case "monovertex-mapstream":
		if !strings.Contains(joined, "--benchmark-streammap") {
			t.Fatal("monovertex-mapstream must use --benchmark-streammap")
		}
	}
}

func assertValidationContent(t *testing.T, id string, in scenario.RenderInput) {
	t.Helper()
	bundle, err := scenario.RenderManifests(in)
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(bundle.AllDocuments(), "\n")
	for _, flag := range []string{"POSTGRES_HOST", "POSTGRES_DB", "POSTGRES_USER", "POSTGRES_PASSWORD", "secretKeyRef"} {
		if !strings.Contains(joined, flag) {
			t.Fatalf("validation manifest missing %s", flag)
		}
	}
	switch id {
	case "map":
		for _, flag := range []string{"--source", "--sourcetransformer", "--unarymap", "--batchmap", "--streammap", "--sink"} {
			if !strings.Contains(joined, flag) {
				t.Fatalf("map validation missing UDF mode %s", flag)
			}
		}
		for _, edge := range []string{`values:`, `"unary-map"`, `"batch-map"`, `"stream-map"`} {
			if !strings.Contains(joined, edge) {
				t.Fatalf("map validation missing routed edge fragment %s", edge)
			}
		}
	case "reduce":
		if !strings.Contains(joined, "--reduce-source") || !strings.Contains(joined, "--reduce") || !strings.Contains(joined, "--reduce-sink") {
			t.Fatal("reduce validation missing reduce UDF modes")
		}
		if !strings.Contains(joined, "fixed:") || !strings.Contains(joined, "PROCESSED_BY") {
			t.Fatal("reduce validation missing fixed window or PROCESSED_BY")
		}
		if !strings.Contains(joined, "fixed-window-reduce") {
			t.Fatal("reduce validation missing fixed-window-reduce vertex")
		}
	case "sliding-reduce":
		if !strings.Contains(joined, "sliding:") || !strings.Contains(joined, "slide: 10s") {
			t.Fatal("sliding-reduce validation missing sliding window config")
		}
		if !strings.Contains(joined, "sliding-window-reduce") {
			t.Fatal("sliding-reduce validation missing sliding-window-reduce vertex")
		}
		if !strings.Contains(joined, "PROCESSED_BY") {
			t.Fatal("sliding-reduce validation missing PROCESSED_BY")
		}
	case "monovertex":
		for _, flag := range []string{
			"--monovertex-source", "--monovertex-transformer", "--monovertex-map",
			"--monovertex-sink", "--monovertex-fallback-sink", "--monovertex-onsuccess-sink",
		} {
			if !strings.Contains(joined, flag) {
				t.Fatalf("monovertex validation missing %s", flag)
			}
		}
		for _, tag := range []string{"bypass-to-fallback", "bypass-to-onsuccess"} {
			if !strings.Contains(joined, tag) {
				t.Fatalf("monovertex validation missing bypass tag %s", tag)
			}
		}
		if !strings.Contains(joined, "kind: MonoVertex") {
			t.Fatal("monovertex validation must render MonoVertex")
		}
	}
}

func TestManifestHashStable(t *testing.T) {
	in := scenario.RenderInput{ScenarioID: "single-map", Kind: scenario.RunKindBenchmark, Namespace: testNS, UDFImage: testUDF}
	a, err := scenario.RenderManifests(in)
	if err != nil {
		t.Fatal(err)
	}
	b, err := scenario.RenderManifests(in)
	if err != nil {
		t.Fatal(err)
	}
	if a.Hash != b.Hash {
		t.Fatal("hash not stable")
	}
}

func TestValidationPostgresHostCrossNamespace(t *testing.T) {
	bundle, err := scenario.RenderManifests(scenario.RenderInput{
		ScenarioID:     "map",
		Kind:           scenario.RunKindValidation,
		Namespace:      "numaflow-validation-deadbeef",
		UDFImage:       testUDF,
		ValidationNS:   validationNS,
		PostgresDBName: testDB,
	})
	if err != nil {
		t.Fatal(err)
	}
	wantHost := central.PostgresServiceName + "." + validationNS + ".svc"
	if !strings.Contains(bundle.Pipeline, wantHost) {
		t.Fatalf("expected POSTGRES host %q in pipeline", wantHost)
	}
}
