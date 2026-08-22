package cli_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"numa-perfman/internal/benchmark"
	"numa-perfman/internal/cli"
)

func TestHelpAndVersion(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	app := cli.App{Stdout: &out, Stderr: &out, Args: []string{"perfman", "help"}}
	if code := app.Run(); code != cli.ExitSuccess {
		t.Fatalf("help code=%d", code)
	}
	if !strings.Contains(out.String(), "Global flags") {
		t.Fatalf("help output: %q", out.String())
	}

	out.Reset()
	app = cli.App{Stdout: &out, Args: []string{"perfman", "version"}, Version: "testver"}
	if code := app.Run(); code != cli.ExitSuccess {
		t.Fatalf("version code=%d", code)
	}
	if !strings.Contains(out.String(), "testver") {
		t.Fatalf("version output: %q", out.String())
	}
}

func TestInvalidCommand(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	app := cli.App{Stdout: &out, Stderr: &out, Args: []string{"perfman", "nope"}}
	if code := app.Run(); code != cli.ExitInvalidUsage {
		t.Fatalf("code=%d want %d", code, cli.ExitInvalidUsage)
	}
}

func TestBenchmarkList(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	app := cli.App{Stdout: &out, Stderr: &out, Args: []string{"perfman", "benchmark", "list"}}
	if code := app.Run(); code != cli.ExitSuccess {
		t.Fatalf("code=%d stderr/out=%q", code, out.String())
	}
	if !strings.Contains(out.String(), "single-map") {
		t.Fatalf("output: %q", out.String())
	}
}

func TestValidationList(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	app := cli.App{Stdout: &out, Stderr: &out, Args: []string{"perfman", "validation", "list"}}
	if code := app.Run(); code != cli.ExitSuccess {
		t.Fatalf("code=%d", code)
	}
	if !strings.Contains(out.String(), "map") {
		t.Fatalf("output: %q", out.String())
	}
}

func TestGlobalFlagsBeforeCommand(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	app := cli.App{
		Stdout: &out, Stderr: &out,
		Args: []string{"perfman", "--cluster", "custom", "benchmark", "list"},
	}
	if code := app.Run(); code != cli.ExitSuccess {
		t.Fatalf("code=%d out=%q", code, out.String())
	}
}

func TestConfigSetAndShowUsesResultsDatabase(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "results", "perfman.db")
	var out bytes.Buffer
	app := cli.App{
		Stdout: &out,
		Stderr: &out,
		Args: []string{
			"perfman", "--results-db", dbPath,
			"config", "set", "--key", "cluster", "--value", "stored-cluster",
		},
	}
	if code := app.Run(); code != cli.ExitSuccess {
		t.Fatalf("config set code=%d output=%q", code, out.String())
	}

	out.Reset()
	app = cli.App{
		Stdout: &out,
		Stderr: &out,
		Args:   []string{"perfman", "--results-db", dbPath, "config", "show"},
	}
	if code := app.Run(); code != cli.ExitSuccess {
		t.Fatalf("config show code=%d output=%q", code, out.String())
	}
	if !strings.Contains(out.String(), `"cluster": "stored-cluster"`) {
		t.Fatalf("config show output=%q", out.String())
	}
	entries, err := os.ReadDir(filepath.Dir(dbPath))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != filepath.Base(dbPath) {
		t.Fatalf("results directory entries=%v; want only %s", entries, filepath.Base(dbPath))
	}
}

func TestBenchmarkRunMissingFlags(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	app := cli.App{Stdout: &out, Stderr: &out, Args: []string{"perfman", "benchmark", "run"}}
	if code := app.Run(); code != cli.ExitInvalidUsage {
		t.Fatalf("code=%d want %d", code, cli.ExitInvalidUsage)
	}
}

func TestBenchmarkReportRequiresResultSelector(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	app := cli.App{
		Stdout: &out,
		Stderr: &out,
		Args:   []string{"perfman", "benchmark", "report", "--scenario", "single-map"},
	}
	if code := app.Run(); code != cli.ExitInvalidUsage {
		t.Fatalf("code=%d want %d; output=%q", code, cli.ExitInvalidUsage, out.String())
	}
}

func TestBenchmarkReportRejectsAggregationFlags(t *testing.T) {
	t.Parallel()
	for _, flag := range []string{"--center", "--percentile-band"} {
		t.Run(flag, func(t *testing.T) {
			var out bytes.Buffer
			app := cli.App{
				Stdout: &out,
				Stderr: &out,
				Args: []string{
					"perfman", "benchmark", "report",
					"--scenario", "single-map",
					"--run-id", "run-id",
					flag, "median",
				},
			}
			if code := app.Run(); code != cli.ExitInvalidUsage {
				t.Fatalf("code=%d want %d; output=%q", code, cli.ExitInvalidUsage, out.String())
			}
		})
	}
}

func TestBenchmarkCompareRejectsUnknownFlags(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	app := cli.App{
		Stdout: &out,
		Stderr: &out,
		Args: []string{
			"perfman", "benchmark", "compare",
			"--scenario", "single-map",
			"--baseline", "quay.io/numaproj/numaflow:v1.7.0",
			"--candidate", "quay.io/numaproj/numaflow:v1.8.0",
			"--format", "png",
		},
	}
	if code := app.Run(); code != cli.ExitInvalidUsage {
		t.Fatalf("code=%d want %d; output=%q", code, cli.ExitInvalidUsage, out.String())
	}
}

func TestBenchmarkRunRejectsRemovedTimingFlags(t *testing.T) {
	t.Parallel()
	for _, flag := range []string{"--repetitions", "--warmup", "--cooldown"} {
		t.Run(flag, func(t *testing.T) {
			var out bytes.Buffer
			app := cli.App{
				Stdout: &out,
				Stderr: &out,
				Args:   []string{"perfman", "benchmark", "run", flag, "1"},
			}
			if code := app.Run(); code != cli.ExitInvalidUsage {
				t.Fatalf("code=%d want %d; output=%q", code, cli.ExitInvalidUsage, out.String())
			}
		})
	}
}

func TestExitCodeMapping(t *testing.T) {
	t.Parallel()
	if cli.ExitCode(nil) != cli.ExitSuccess {
		t.Fatal("nil should be success")
	}
	if cli.ExitCode(cli.ErrUsageForTest()) != cli.ExitInvalidUsage {
		t.Fatal("usage mapping")
	}
	if cli.ExitCode(benchmark.ErrOverwriteDeclined) != cli.ExitSuccess {
		t.Fatal("overwrite declined should be a successful cancellation")
	}
}
