//go:build integration

package integration

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"numa-perfman/internal/benchmark"
	"numa-perfman/internal/cli"
	"numa-perfman/internal/namespace"
	"numa-perfman/internal/results"
)

// §16.3 (13): interrupted benchmark releases the database lock row.
func TestInterruptReleasesLock(t *testing.T) {
	e := shared(t)
	image := requireBenchmarkImage(t)
	_ = e.loadImage(image)

	key, err := namespace.ParseBenchmarkKey("single-map", image)
	if err != nil {
		t.Fatal(err)
	}
	lockKey := benchmark.BenchmarkLockKey(key)
	bin := filepath.Join(e.root, "bin", "perfman")
	args := append([]string{
		"--cluster", e.cluster,
		"--context", e.context,
		"benchmark", "run",
		"--scenario", "single-map",
		"--image", image,
		"--duration", "10m",
	})

	cmd := exec.Command(bin, args...)
	cmd.Dir = e.workDir
	cmd.Env = append(os.Environ(), e.configEnv()...)
	var runOut bytes.Buffer
	cmd.Stdout = &runOut
	cmd.Stderr = &runOut
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	time.Sleep(15 * time.Second)
	if err := cmd.Process.Signal(syscall.SIGINT); err != nil {
		t.Fatal(err)
	}
	waitErr := cmd.Wait()
	if waitErr == nil {
		t.Fatal("expected interrupted benchmark process to exit non-zero")
	}
	if ee, ok := waitErr.(*exec.ExitError); ok && ee.ExitCode() != cli.ExitInterruptedSIGINT {
		t.Fatalf("exit code %d want %d", ee.ExitCode(), cli.ExitInterruptedSIGINT)
	}
	if ns := lastNamespaceFromOutput(runOut.String()); ns != "" {
		t.Cleanup(func() {
			_, _ = e.kubectl("delete", "namespace", ns, "--ignore-not-found")
		})
	}

	ctx := context.Background()
	repo, err := e.openResults(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()

	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		_, held, err := repo.ReadActiveLock(ctx, lockKey)
		if err != nil {
			t.Fatal(err)
		}
		if !held {
			return
		}
		time.Sleep(500 * time.Millisecond)
	}
	lock, held, _ := repo.ReadActiveLock(ctx, lockKey)
	if held {
		t.Fatalf("active lock still held after interrupt: %+v", lock)
	}
}

// §16.3 (13): stale lock removal works when owning PID is gone and namespace is inactive.
func TestStaleLockRecoverable(t *testing.T) {
	e := shared(t)
	ctx := context.Background()
	repo, err := e.openResults(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()

	lockKey := "benchmark:single-map:v1-8-0"
	if err := repo.AcquireActiveLock(ctx, results.AcquireActiveLockParams{
		LockKey:    lockKey,
		OwnerToken: "integration-stale-probe",
		PID:        99999999,
		Command:    "integration stale probe",
		Namespace:  "perf-single-map-v1-8-0",
		Scenario:   "single-map",
	}); err != nil {
		t.Fatal(err)
	}

	code, out := e.runCLI("cleanup", "--remove-stale-lock")
	if code != cli.ExitSuccess {
		t.Fatalf("cleanup exit=%d:\n%s", code, out)
	}
	if !strings.Contains(out, "removed stale lock") {
		t.Fatalf("expected stale lock removal message, got:\n%s", out)
	}
	_, held, err := repo.ReadActiveLock(ctx, lockKey)
	if err != nil {
		t.Fatal(err)
	}
	if held {
		t.Fatal("expected stale lock row removed from active_locks")
	}
}
