package results

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func assertNoDBSidecars(t *testing.T, dbPath string) {
	t.Helper()
	dir := filepath.Dir(dbPath)
	base := filepath.Base(dbPath)
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read db directory: %v", err)
	}
	for _, e := range entries {
		name := e.Name()
		if name == base {
			continue
		}
		if strings.HasPrefix(name, base+"-") ||
			strings.HasSuffix(name, "-wal") ||
			strings.HasSuffix(name, "-shm") ||
			strings.HasSuffix(name, "-journal") {
			t.Fatalf("unexpected sqlite sidecar file %q", name)
		}
	}
}

func openTestRepo(t *testing.T) (*Repository, string) {
	t.Helper()
	ctx := context.Background()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "perfman.db")
	repo, err := Open(ctx, dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = repo.Close() })
	return repo, dbPath
}

func createTestRun(t *testing.T, repo *Repository, kind string) string {
	t.Helper()
	runID := uuid.NewString()
	if err := repo.CreateRun(context.Background(), CreateRunParams{
		ID: runID, Kind: kind, Scenario: "single-map",
		ImageRef: "quay.io/numaproj/numaflow:v1.8.0", Status: StatusCreated,
	}); err != nil {
		t.Fatal(err)
	}
	return runID
}

func TestOpenUsesMemoryJournalWithoutSidecars(t *testing.T) {
	repo, dbPath := openTestRepo(t)
	ctx := context.Background()

	var mode string
	if err := repo.db.QueryRowContext(ctx, `PRAGMA journal_mode`).Scan(&mode); err != nil {
		t.Fatal(err)
	}
	if mode != "memory" {
		t.Fatalf("journal_mode=%q want memory", mode)
	}
	var autoVacuum int
	if err := repo.db.QueryRowContext(ctx, `PRAGMA auto_vacuum`).Scan(&autoVacuum); err != nil {
		t.Fatal(err)
	}
	if autoVacuum != 2 {
		t.Fatalf("auto_vacuum=%d want 2 (incremental)", autoVacuum)
	}

	if err := repo.UpsertHarnessConfig(ctx, HarnessConfig{
		Cluster: "numaflow", LogFormat: "text", UpdatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}
	assertNoDBSidecars(t, dbPath)
}

func TestHarnessConfigUpsertAndLoad(t *testing.T) {
	repo, dbPath := openTestRepo(t)
	ctx := context.Background()

	_, found, err := repo.LoadHarnessConfig(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if found {
		t.Fatal("expected no config initially")
	}

	updated := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	want := HarnessConfig{
		Context:                  "kind-numaflow",
		Cluster:                  "numaflow",
		PrometheusURL:            "http://localhost:9090",
		CentralNamespace:         "numaflow-system",
		MonitoringNamespace:      "monitoring",
		ValidationNamespace:      "validation-system",
		LogFormat:                "json",
		Verbose:                  true,
		UDFImage:                 "numaflow-perfman-udfs:abc",
		Image:                    "quay.io/numaproj/numaflow:v1.8.2",
		TestedNumaflowMajorMinor: "1.8",
		UpdatedAt:                updated,
	}
	if err := repo.UpsertHarnessConfig(ctx, want); err != nil {
		t.Fatal(err)
	}
	got, found, err := repo.LoadHarnessConfig(ctx)
	if err != nil || !found {
		t.Fatalf("load config found=%v err=%v", found, err)
	}
	if got.Cluster != want.Cluster || got.LogFormat != want.LogFormat || !got.Verbose {
		t.Fatalf("config mismatch: %#v", got)
	}
	if !got.UpdatedAt.Equal(updated) {
		t.Fatalf("updated_at=%v want %v", got.UpdatedAt, updated)
	}
	assertNoDBSidecars(t, dbPath)
}

func TestRunArtifactBlobRoundtripAndCascade(t *testing.T) {
	repo, dbPath := openTestRepo(t)
	ctx := context.Background()
	runID := createTestRun(t, repo, KindBenchmark)

	content := []byte("apiVersion: v1\nkind: ConfigMap\nbinary:\n  \x00\xff\xfe")
	put, err := repo.PutRunArtifact(ctx, PutRunArtifactParams{
		RunID: runID, Kind: "manifest", Name: "pipeline.yaml",
		MediaType: "application/yaml", Content: content,
	})
	if err != nil {
		t.Fatal(err)
	}
	if put.SizeBytes != int64(len(content)) || put.SHA256 == "" {
		t.Fatalf("artifact stats: size=%d sha=%q", put.SizeBytes, put.SHA256)
	}

	listed, err := repo.ListRunArtifacts(ctx, runID)
	if err != nil || len(listed) != 1 {
		t.Fatalf("list artifacts len=%d err=%v", len(listed), err)
	}
	if listed[0].Kind != "manifest" || listed[0].SizeBytes != put.SizeBytes {
		t.Fatalf("listed meta: %#v", listed[0])
	}

	got, err := repo.GetRunArtifact(ctx, runID, "manifest", "pipeline.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if string(got.Content) != string(content) {
		t.Fatalf("content roundtrip failed")
	}

	replaced := []byte("replaced manifest")
	if _, err := repo.PutRunArtifact(ctx, PutRunArtifactParams{
		RunID: runID, Kind: "manifest", Name: "pipeline.yaml", Content: replaced,
	}); err != nil {
		t.Fatal(err)
	}
	got2, err := repo.GetRunArtifact(ctx, runID, "manifest", "pipeline.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if string(got2.Content) != string(replaced) {
		t.Fatal("upsert did not replace content")
	}

	if err := repo.DeleteRun(ctx, runID); err != nil {
		t.Fatal(err)
	}
	listed, err = repo.ListRunArtifacts(ctx, runID)
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 0 {
		t.Fatalf("expected cascade delete, got %d artifacts", len(listed))
	}
	assertNoDBSidecars(t, dbPath)
}

func TestValidationFailureSamplesReplaceAndLoad(t *testing.T) {
	repo, dbPath := openTestRepo(t)
	ctx := context.Background()
	runID := createTestRun(t, repo, KindValidation)

	samples := []ValidationFailureSample{
		{Kind: "missing", LogicalKey: "k1", Expected: "a", Actual: "", Detail: "not found"},
		{Kind: "corrupted", LogicalKey: "k2", Expected: "x", Actual: "y", Detail: "payload mismatch"},
	}
	if err := repo.ReplaceValidationFailureSamples(ctx, runID, samples); err != nil {
		t.Fatal(err)
	}
	got, err := repo.LoadValidationFailureSamples(ctx, runID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Kind != "missing" || got[1].LogicalKey != "k2" {
		t.Fatalf("samples: %#v", got)
	}

	replaced := []ValidationFailureSample{{Kind: "unexpected", LogicalKey: "k3"}}
	if err := repo.ReplaceValidationFailureSamples(ctx, runID, replaced); err != nil {
		t.Fatal(err)
	}
	got, err = repo.LoadValidationFailureSamples(ctx, runID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Kind != "unexpected" {
		t.Fatalf("replace samples: %#v", got)
	}

	if err := repo.DeleteRun(ctx, runID); err != nil {
		t.Fatal(err)
	}
	got, err = repo.LoadValidationFailureSamples(ctx, runID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("expected cascade delete, got %d samples", len(got))
	}
	assertNoDBSidecars(t, dbPath)
}

func TestValidationResultStructuredFieldsRoundtrip(t *testing.T) {
	repo, _ := openTestRepo(t)
	ctx := context.Background()
	runID := createTestRun(t, repo, KindValidation)
	want := ValidationResult{
		RunID: runID, Passed: false, SourceCount: 10, ExpectedCount: 9,
		LogicalOutputCount: 8, PhysicalDeliveryCount: 12, DuplicateDeliveryCount: 4,
		DuplicateRate: 0.25, MissingCount: 1, UnexpectedCount: 2, CorruptedCount: 3,
		RoutingMismatchCount: 5, ChildMismatchCount: 6, DetailJSON: "{}",
	}
	if err := repo.SaveValidationResult(ctx, want); err != nil {
		t.Fatal(err)
	}
	got, found, err := repo.GetValidationResult(ctx, runID)
	if err != nil || !found {
		t.Fatalf("get validation result found=%v err=%v", found, err)
	}
	if got.DuplicateRate != want.DuplicateRate ||
		got.RoutingMismatchCount != want.RoutingMismatchCount ||
		got.ChildMismatchCount != want.ChildMismatchCount {
		t.Fatalf("structured fields got=%+v want=%+v", got, want)
	}
}

func TestReportSaveListGetAndCascade(t *testing.T) {
	repo, dbPath := openTestRepo(t)
	ctx := context.Background()
	runA := createTestRun(t, repo, KindBenchmark)
	runB := uuid.NewString()
	if err := repo.CreateRun(context.Background(), CreateRunParams{
		ID: runB, Kind: KindBenchmark, Scenario: "single-map",
		ImageRef: "quay.io/numaproj/numaflow:v1.7.0", Status: StatusCreated,
	}); err != nil {
		t.Fatal(err)
	}

	payload := []byte(`{"scenario":"single-map","metrics":[]}`)
	generated := time.Date(2026, 8, 1, 13, 0, 0, 0, time.UTC)
	saved, err := repo.SaveReport(ctx, SaveReportParams{
		Kind: ReportKindComparison, Scenario: "single-map", GeneratedAt: generated,
		SelectionJSON: `{"baseline_latest_complete":1}`, Payload: payload,
		BaselineImageRef: "img:a", CandidateImageRef: "img:b",
		RunRefs: []ReportRunRef{
			{RunID: runA, Group: "baseline"},
			{RunID: runB, Group: "candidate"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if saved.SHA256 == "" || saved.SizeBytes != int64(len(payload)) {
		t.Fatalf("report stats: size=%d sha=%q", saved.SizeBytes, saved.SHA256)
	}

	summaries, err := repo.ListReports(ctx, ListReportsFilter{Scenario: "single-map"})
	if err != nil || len(summaries) != 1 {
		t.Fatalf("list reports len=%d err=%v", len(summaries), err)
	}

	got, err := repo.GetReport(ctx, saved.ID)
	if err != nil {
		t.Fatal(err)
	}
	if string(got.Payload) != string(payload) || len(got.RunRefs) != 2 {
		t.Fatalf("report roundtrip: payload=%q refs=%d", got.Payload, len(got.RunRefs))
	}

	if err := repo.DeleteRun(ctx, runA); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.GetReport(ctx, saved.ID); err == nil {
		t.Fatal("expected report cascade when linked run deleted")
	}
	assertNoDBSidecars(t, dbPath)
}

func TestActiveLockOwnershipAndContention(t *testing.T) {
	repo, dbPath := openTestRepo(t)
	ctx := context.Background()
	key := "benchmark:single-map:v1.8.0"
	ownerA := uuid.NewString()
	ownerB := uuid.NewString()

	if err := repo.AcquireActiveLock(ctx, AcquireActiveLockParams{
		LockKey: key, OwnerToken: ownerA, PID: 100, Host: "host-a", Command: "benchmark run",
		RunID: uuid.NewString(), Namespace: "numaflow-perf-abc", Scenario: "single-map",
		ImageRef: "quay.io/numaproj/numaflow:v1.8.0",
	}); err != nil {
		t.Fatal(err)
	}

	err := repo.AcquireActiveLock(ctx, AcquireActiveLockParams{
		LockKey: key, OwnerToken: ownerB, PID: 200,
	})
	if !errors.Is(err, ErrActiveLockHeld) {
		t.Fatalf("expected ErrActiveLockHeld, got %v", err)
	}

	lock, found, err := repo.ReadActiveLock(ctx, key)
	if err != nil || !found || lock.OwnerToken != ownerA {
		t.Fatalf("read lock found=%v owner=%q err=%v", found, lock.OwnerToken, err)
	}

	newPID := 101
	if err := repo.UpdateActiveLock(ctx, key, ownerA, UpdateActiveLockParams{PID: &newPID}); err != nil {
		t.Fatal(err)
	}
	lock, _, err = repo.ReadActiveLock(ctx, key)
	if err != nil || lock.PID != 101 {
		t.Fatalf("update lock pid=%d err=%v", lock.PID, err)
	}

	if err := repo.UpdateActiveLock(ctx, key, ownerB, UpdateActiveLockParams{PID: &newPID}); !errors.Is(err, ErrActiveLockNotHeld) {
		t.Fatalf("expected ErrActiveLockNotHeld on wrong owner, got %v", err)
	}

	if err := repo.ReleaseActiveLock(ctx, key, ownerA); err != nil {
		t.Fatal(err)
	}
	_, found, err = repo.ReadActiveLock(ctx, key)
	if err != nil || found {
		t.Fatalf("expected lock released, found=%v err=%v", found, err)
	}

	if err := repo.AcquireActiveLock(ctx, AcquireActiveLockParams{LockKey: key, OwnerToken: ownerB, PID: 200}); err != nil {
		t.Fatal(err)
	}
	locks, err := repo.ListActiveLocks(ctx)
	if err != nil || len(locks) != 1 || locks[0].OwnerToken != ownerB {
		t.Fatalf("list locks: %#v err=%v", locks, err)
	}
	if err := repo.DeleteActiveLock(ctx, key, ownerB); err != nil {
		t.Fatal(err)
	}
	assertNoDBSidecars(t, dbPath)
}

func TestOpenInitializesStorageTables(t *testing.T) {
	repo, _ := openTestRepo(t)
	ctx := context.Background()

	tables := []string{
		"harness_config", "run_artifacts", "validation_failure_samples",
		"reports", "report_runs", "active_locks",
	}
	for _, table := range tables {
		var name string
		err := repo.db.QueryRowContext(ctx, `
			SELECT name FROM sqlite_master WHERE type='table' AND name=?`, table,
		).Scan(&name)
		if err != nil {
			t.Fatalf("table %q missing: %v", table, err)
		}
	}
}
