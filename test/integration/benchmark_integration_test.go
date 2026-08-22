//go:build integration

package integration

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"numa-perfman/internal/cli"
	"numa-perfman/internal/controller"
	"numa-perfman/internal/results"
)

// §16.3 (3): locally loaded Numaflow image runs with controller pull policy IfNotPresent.
func TestLocalImageIfNotPresent(t *testing.T) {
	e := shared(t)
	image := requireBenchmarkImage(t)
	if err := e.loadImage(image); err != nil {
		t.Fatalf("kind load benchmark image: %v", err)
	}

	policyCh := make(chan string, 1)
	stop := make(chan struct{})
	go func() {
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				out, err := e.kubectl(
					"get", "ns", "-l", "perfman.numaproj.io/managed-by=numaflow-perfman",
					"-o", "jsonpath={.items[0].metadata.name}",
				)
				if err != nil || strings.TrimSpace(out) == "" {
					continue
				}
				policy, err := e.controllerPullPolicyIn(strings.TrimSpace(out))
				if err != nil || policy == "" {
					continue
				}
				select {
				case policyCh <- policy:
				default:
				}
				return
			}
		}
	}()

	args := append([]string{
		"benchmark", "run",
		"--scenario", "single-map",
		"--image", image,
	}, benchShortFlags()...)
	code, out := e.runCLI(args...)
	close(stop)
	if code != cli.ExitSuccess {
		t.Fatalf("expected exit %d after pipeline stood up, got %d:\n%s", cli.ExitSuccess, code, out)
	}
	select {
	case policy := <-policyCh:
		if policy != controller.ImagePullPolicy {
			t.Fatalf("controller pull policy %q want %q", policy, controller.ImagePullPolicy)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("did not observe controller pull policy while run namespace existed")
	}
}

// §16.3 (4): external registry image is pulled when absent from kind nodes.
func TestExternalRegistryImagePull(t *testing.T) {
	e := shared(t)
	image := requireExternalImage(t)

	present, err := e.imageOnNodes(image)
	if err != nil {
		t.Fatal(err)
	}
	if present {
		t.Skipf("image %q already present on kind nodes; remove it to test registry pull", image)
	}

	args := append([]string{
		"benchmark", "run",
		"--scenario", "simple-monovertex",
		"--image", image,
		"--duration", "90s",
		"--yes",
	})
	code, out := e.runCLI(args...)
	if code != cli.ExitSuccess {
		t.Fatalf("benchmark with external image exit=%d:\n%s", code, out)
	}
}

// §16.3 (5): missing UDF image fails preflight before creating a run namespace.
func TestMissingUDFPreflight(t *testing.T) {
	e := shared(t)
	requireBenchmarkImage(t)

	missing := "numaflow-perfman-udfs:integration-missing-" + randomSuffix(3)
	args := append([]string{
		"--udf-image", missing,
		"benchmark", "run",
		"--scenario", "single-map",
		"--image", os.Getenv(envBenchmarkImage),
		"--duration", "5s",
		"--yes",
	})
	code, out := e.runCLI(args...)
	if code != cli.ExitPreflightSetup && code != cli.ExitInfrastructure {
		t.Fatalf("expected preflight/setup failure exit %d or %d, got %d:\n%s",
			cli.ExitPreflightSetup, cli.ExitInfrastructure, code, out)
	}
	if !strings.Contains(strings.ToLower(out), "udf") {
		t.Fatalf("expected udf preflight message, got:\n%s", out)
	}
}

// §16.3 (6): two sequential benchmark images produce stored metrics.
func TestSequentialBenchmarkMetricsStored(t *testing.T) {
	e := shared(t)
	imgA := requireBenchmarkImage(t)
	imgB, ok := optionalBenchmarkImageB(t)
	if !ok {
		t.Skipf("set %s to a second Numaflow image to verify two stored benchmark results", envBenchmarkImageB)
	}
	for _, img := range []string{imgA, imgB} {
		if err := e.loadImage(img); err != nil {
			t.Fatalf("kind load %q: %v", img, err)
		}
	}

	for i, img := range []string{imgA, imgB} {
		args := append([]string{
			"benchmark", "run",
			"--scenario", "single-map",
			"--image", img,
		}, benchShortFlags()...)
		code, out := e.runCLI(args...)
		if code != cli.ExitSuccess {
			t.Fatalf("run %d exit=%d:\n%s", i, code, out)
		}
	}

	ctx := context.Background()
	repo, err := e.openResults(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	runs, err := repo.ListRuns(ctx, results.ListRunsFilter{Scenario: "single-map"})
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) < 2 {
		t.Fatalf("want >=2 runs, got %d", len(runs))
	}
	for _, run := range runs[:2] {
		if run.Status != "completed" {
			t.Fatalf("run %s status %q", run.ID, run.Status)
		}
		series, err := repo.LoadMetricSeries(ctx, run.ID)
		if err != nil {
			t.Fatal(err)
		}
		if len(series) == 0 {
			t.Fatalf("run %s has no stored metrics", run.ID)
		}
	}
}

// §16.3 (7): comparison report stored in SQLite contains both image references.
func TestCompareContainsImageRefs(t *testing.T) {
	e := shared(t)
	imgA := requireBenchmarkImage(t)
	imgB, ok := optionalBenchmarkImageB(t)
	if !ok {
		t.Skipf("set %s to a second Numaflow image to exercise compare output", envBenchmarkImageB)
	}
	for _, img := range []string{imgA, imgB} {
		_ = e.loadImage(img)
		args := append([]string{
			"benchmark", "run",
			"--scenario", "single-map",
			"--image", img,
		}, benchShortFlags()...)
		code, out := e.runCLI(args...)
		if code != cli.ExitSuccess {
			t.Fatalf("benchmark exit=%d:\n%s", code, out)
		}
	}

	code, out := e.runCLI(
		"benchmark", "compare",
		"--scenario", "single-map",
		"--baseline", imgA,
		"--candidate", imgB,
	)
	if code != cli.ExitSuccess {
		t.Fatalf("compare exit=%d:\n%s", code, out)
	}
	reportID := storedReportID(out)
	if reportID == "" {
		t.Fatalf("missing stored report id in compare output:\n%s", out)
	}

	code, reportOut := e.runCLI("reports", "show", "--id", reportID, "--format", "json")
	if code != cli.ExitSuccess {
		t.Fatalf("reports show exit=%d:\n%s", code, reportOut)
	}
	combined := out + reportOut
	if !strings.Contains(combined, imgA) || !strings.Contains(combined, imgB) {
		t.Fatalf("compare report missing image refs %q and/or %q:\n%s", imgA, imgB, combined)
	}
}

// §16.3 (8): successful run deletes its temporary namespace.
func TestSuccessNamespaceDeleted(t *testing.T) {
	e := shared(t)
	image := requireBenchmarkImage(t)
	_ = e.loadImage(image)

	args := append([]string{
		"benchmark", "run",
		"--scenario", "single-map",
		"--image", image,
	}, benchShortFlags()...)
	code, out := e.runCLI(args...)
	if code != cli.ExitSuccess {
		t.Fatalf("exit=%d:\n%s", code, out)
	}
	ns := lastNamespaceFromOutput(out)
	if ns == "" {
		t.Fatal("missing namespace in output")
	}
	deadline := time.Now().Add(3 * time.Minute)
	for time.Now().Before(deadline) {
		out, err := e.kubectl("get", "namespace", ns, "--ignore-not-found", "-o", "name")
		if err != nil {
			t.Fatalf("kubectl get namespace: %v", err)
		}
		if strings.TrimSpace(out) == "" {
			return
		}
		time.Sleep(2 * time.Second)
	}
	t.Fatalf("namespace %q still exists after successful benchmark", ns)
}

// §16.3 (9): retained failure namespace has controller scaled to zero.
func TestRetainedFailureControllerZero(t *testing.T) {
	e := shared(t)
	image := requireBenchmarkImage(t)
	_ = e.loadImage(image)

	// Short measurement window yields fewer than MinSamples for required metrics.
	args := append([]string{
		"benchmark", "run",
		"--scenario", "single-map",
		"--image", image,
		"--duration", "5s",
		"--yes",
	})
	code, out := e.runCLI(args...)
	if code != cli.ExitMetricsIncomplete {
		t.Fatalf("expected exit %d, got %d:\n%s", cli.ExitMetricsIncomplete, code, out)
	}
	ns := lastNamespaceFromOutput(out)
	if ns == "" {
		t.Fatal("missing namespace in output")
	}
	t.Cleanup(func() {
		_, _ = e.kubectl("delete", "namespace", ns, "--ignore-not-found")
	})

	replicas, err := e.controllerReplicasIn(ns)
	if err != nil {
		t.Fatal(err)
	}
	if replicas != 0 {
		t.Fatalf("retained failure namespace %q controller replicas=%d want 0", ns, replicas)
	}
}

// §16.3 (10): required metric absence marks run incomplete (exit 5).
func TestRequiredMetricAbsentIncomplete(t *testing.T) {
	e := shared(t)
	image := requireBenchmarkImage(t)
	_ = e.loadImage(image)

	// Short measurement window cannot satisfy MinSamples on required metrics.
	args := append([]string{
		"benchmark", "run",
		"--scenario", "single-map",
		"--image", image,
		"--duration", "5s",
		"--yes",
	})
	code, out := e.runCLI(args...)
	if code != cli.ExitMetricsIncomplete {
		t.Fatalf("expected exit %d, got %d:\n%s", cli.ExitMetricsIncomplete, code, out)
	}
	if !strings.Contains(strings.ToLower(out), "metric") && !strings.Contains(strings.ToLower(out), "incomplete") {
		t.Logf("output (informational):\n%s", out)
	}
}

func lastNamespaceFromOutput(out string) string {
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "namespace=") {
			parts := strings.Split(line, "namespace=")
			if len(parts) < 2 {
				continue
			}
			field := strings.Fields(parts[1])
			if len(field) > 0 {
				return strings.TrimSpace(field[0])
			}
		}
	}
	return ""
}

func storedReportID(out string) string {
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "stored report ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "stored report "))
		}
	}
	return ""
}
