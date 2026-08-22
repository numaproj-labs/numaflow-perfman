package serve

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"numa-perfman/internal/benchmark"
	"numa-perfman/internal/cluster"
	"numa-perfman/internal/config"
	"numa-perfman/internal/namespace"
	"numa-perfman/internal/results"
)

func TestJobManagerAllowsParallelDifferentTags(t *testing.T) {
	mgr := newTestJobManager(t)
	injectRunningJob(t, mgr, "run-a", "single-map", "quay.io/numaproj/numaflow:v1.8.1")

	info, err := mgr.Start(context.Background(), StartRunRequest{
		Scenario: "single-map",
		Image:    "quay.io/numaproj/numaflow:v1.7.0",
		Duration: "1m",
	})
	if err != nil {
		t.Fatalf("expected parallel start to succeed: %v", err)
	}
	if info.ID == "" {
		t.Fatal("expected run id")
	}
	_ = mgr.Cancel(info.ID)
	_ = mgr.WaitDone(context.Background(), info.ID)
}

func TestJobManagerBlocksSameScenarioAndTag(t *testing.T) {
	mgr := newTestJobManager(t)
	injectRunningJob(t, mgr, "run-a", "single-map", "quay.io/numaproj/numaflow:v1.8.1")

	_, err := mgr.Start(context.Background(), StartRunRequest{
		Scenario: "single-map",
		Image:    "quay.io/numaproj/numaflow:v1.8.1",
		Duration: "1m",
	})
	var conflict *JobConflictError
	if !errors.As(err, &conflict) {
		t.Fatalf("expected JobConflictError, got %v", err)
	}
	if conflict.Code != "lock_held" {
		t.Fatalf("code=%q", conflict.Code)
	}
	if conflict.ExistingRun == nil || conflict.ExistingRun.ID != "run-a" {
		t.Fatalf("existing_run=%v", conflict.ExistingRun)
	}
}

func TestJobManagerListMarksMultipleActive(t *testing.T) {
	mgr := newTestJobManager(t)
	injectRunningJob(t, mgr, "run-a", "single-map", "quay.io/numaproj/numaflow:v1.8.1")
	injectRunningJob(t, mgr, "run-b", "single-map", "quay.io/numaproj/numaflow:v1.7.0")

	ctx := context.Background()
	for _, id := range []string{"run-a", "run-b"} {
		job := mgr.jobs[id]
		if err := mgr.repo.CreateRun(ctx, results.CreateRunParams{
			ID: id, Kind: results.KindBenchmark, Scenario: job.scenario,
			ImageRef: job.image, Status: results.StatusMeasuring,
			ManifestHash: "h", ConfigJSON: `{}`, CreatedAt: time.Now().UTC(),
		}); err != nil {
			t.Fatal(err)
		}
	}

	list, err := mgr.List(ctx, "single-map", "", 10)
	if err != nil {
		t.Fatal(err)
	}
	active := map[string]bool{}
	for _, info := range list {
		active[info.ID] = info.Active
	}
	if !active["run-a"] || !active["run-b"] {
		t.Fatalf("active flags=%v", active)
	}
}

func newTestJobManager(t *testing.T) *JobManager {
	t.Helper()
	dir := t.TempDir()
	repo, err := results.Open(context.Background(), filepath.Join(dir, "results.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = repo.Close() })
	cfg := config.Defaults()
	cfg.UDFImage = "udf:local"
	return NewJobManager(repo, cfg, cluster.Client{}, "test")
}

func injectRunningJob(t *testing.T, m *JobManager, runID, scenario, image string) {
	t.Helper()
	key, err := namespace.ParseBenchmarkKey(scenario, image)
	if err != nil {
		t.Fatal(err)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.jobs[runID] = &activeJob{
		cancel:   func() {},
		runID:    runID,
		scenario: scenario,
		image:    image,
		lockKey:  benchmark.BenchmarkLockKey(key),
		hub:      newEventHub(),
		done:     make(chan struct{}),
	}
}
