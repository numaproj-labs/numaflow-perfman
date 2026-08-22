//go:build integration

package integration

import (
	"strings"
	"testing"

	"numa-perfman/internal/cli"
)

// §16.4 end-to-end acceptance: clean cluster bootstrap, benchmarks, compare reports, all validations, clean exit.
func TestAcceptanceSuite(t *testing.T) {
	e := shared(t)
	requireFullAcceptance(t)
	imgA := requireBenchmarkImage(t)
	imgB, ok := optionalBenchmarkImageB(t)
	if !ok {
		t.Fatalf("%s is required for acceptance compare (second Numaflow image)", envBenchmarkImageB)
	}
	for _, img := range []string{imgA, imgB} {
		if err := e.loadImage(img); err != nil {
			t.Fatalf("kind load %q: %v", img, err)
		}
	}

	for _, img := range []string{imgA, imgB} {
		code, out := e.runCLI(append([]string{
			"benchmark", "run",
			"--scenario", "single-map",
			"--image", img,
			"--duration", "15m",
			"--yes",
		})...)
		if code != cli.ExitSuccess {
			t.Fatalf("single-map benchmark for %s exit=%d:\n%s", img, code, out)
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
		t.Fatalf("missing stored report id:\n%s", out)
	}
	for _, format := range []string{"json", "html"} {
		code, showOut := e.runCLI("reports", "show", "--id", reportID, "--format", format)
		if code != cli.ExitSuccess {
			t.Fatalf("reports show %s exit=%d:\n%s", format, code, showOut)
		}
		if !strings.Contains(showOut, imgA) || !strings.Contains(showOut, imgB) {
			t.Fatalf("report %s missing image refs %q / %q:\n%s", format, imgA, imgB, showOut)
		}
	}

	code, out = e.runCLI(
		"validation", "run",
		"--scenario", "map",
		"--image", imgA,
		"--events", "0",
		"--tier", "standard",
		"--timeout", "120m",
	)
	if code != cli.ExitSuccess {
		t.Fatalf("map validation exit=%d:\n%s", code, out)
	}

	// Preflight failure path: invalid scenario id via CLI usage (exit 2), not masked as success.
	code, out = e.runCLI("benchmark", "run", "--scenario", "not-a-scenario", "--image", imgA)
	if code != cli.ExitInvalidUsage {
		t.Fatalf("invalid scenario exit=%d want %d:\n%s", code, cli.ExitInvalidUsage, out)
	}

	waitForNoActiveRunNamespaces(t, e)
}
