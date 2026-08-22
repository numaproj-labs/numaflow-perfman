package results

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestRepositoryLifecycleAndMetrics(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "perfman.db")

	repo, err := Open(ctx, dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = repo.Close() })

	runID := uuid.NewString()
	now := time.Now().UTC().Truncate(time.Millisecond)
	measureStart := now.Add(5 * time.Minute)

	cfg := `{"duration":"15m","resources":{"cpu":"1"}}`
	if err := repo.CreateRun(ctx, CreateRunParams{
		ID: runID, Kind: KindBenchmark, Scenario: "single-map",
		ImageRef: "quay.io/numaproj/numaflow:v1.7.0",
		Status: StatusCreated, ManifestHash: "abc123", ConfigJSON: cfg,
	}); err != nil {
		t.Fatal(err)
	}

	if err := repo.UpdateRunLifecycle(ctx, runID, UpdateRunLifecycleParams{
		Status:               StatusMeasuring,
		MeasurementStartedAt: &measureStart,
	}); err != nil {
		t.Fatal(err)
	}

	if err := repo.AddEvent(ctx, AddEventParams{
		RunID: runID, Timestamp: now, Level: "info", Phase: "measuring", Message: "started",
	}); err != nil {
		t.Fatal(err)
	}

	points := []MetricPoint{
		{Timestamp: measureStart, ElapsedMilliseconds: 0, Value: 10},
		{Timestamp: measureStart.Add(10 * time.Second), ElapsedMilliseconds: 10000, Value: 12},
	}
	if err := repo.SaveMetrics(ctx, SaveMetricsParams{
		RunID:           runID,
		RequiredMetrics: []string{"throughput", "latency"},
		Series: []MetricSeriesInput{
			{MetricName: "throughput", Unit: "events/s", Points: points},
			{MetricName: "latency", Unit: "s", Points: points},
		},
	}); err != nil {
		t.Fatal(err)
	}

	if err := repo.SaveMetrics(ctx, SaveMetricsParams{
		RunID: runID, RequiredMetrics: []string{"throughput"},
		Series: []MetricSeriesInput{{MetricName: "throughput", Points: points}},
	}); err == nil {
		t.Fatal("expected missing required metric error")
	}

	completed := measureStart.Add(15 * time.Minute)
	if err := repo.UpdateRunLifecycle(ctx, runID, UpdateRunLifecycleParams{
		Status: StatusCompleted, CompletedAt: &completed,
	}); err != nil {
		t.Fatal(err)
	}

	got, err := repo.GetRun(ctx, runID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != StatusCompleted || got.Scenario != "single-map" {
		t.Fatalf("run: %#v", got)
	}

	series, err := repo.LoadMetricSeries(ctx, runID)
	if err != nil {
		t.Fatal(err)
	}
	if len(series) != 2 {
		t.Fatalf("series count %d", len(series))
	}

	listed, err := repo.ListRuns(ctx, ListRunsFilter{Scenario: "single-map", ImageRef: got.ImageRef})
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 1 {
		t.Fatalf("list len %d", len(listed))
	}

	if err := repo.DeleteRun(ctx, runID); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.GetRun(ctx, runID); err == nil {
		t.Fatal("expected missing run")
	}
}

func TestNormalizeJSONRejectsInvalid(t *testing.T) {
	if _, err := normalizeJSON("{not json", true); err == nil {
		t.Fatal("expected error")
	}
}

func TestOpenInitializesFinalSchemaOnly(t *testing.T) {
	ctx := context.Background()
	repo, err := Open(ctx, filepath.Join(t.TempDir(), "perfman.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = repo.Close() })

	var id string
	if err := repo.db.QueryRowContext(ctx, `
		SELECT id
		FROM runs
		LIMIT 1`).Scan(&id); err != sql.ErrNoRows {
		t.Fatalf("final runs schema query error=%v", err)
	}

	var migrationTables int
	if err := repo.db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM sqlite_master
		WHERE type = 'table' AND name = 'results_schema_migrations'`).Scan(&migrationTables); err != nil {
		t.Fatal(err)
	}
	if migrationTables != 0 {
		t.Fatalf("migration table count=%d want 0", migrationTables)
	}
}

func TestRepositoryStoresOneBenchmarkPerScenarioAndImage(t *testing.T) {
	ctx := context.Background()
	repo, err := Open(ctx, filepath.Join(t.TempDir(), "perfman.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = repo.Close() })

	params := CreateRunParams{
		ID: uuid.NewString(), Kind: KindBenchmark, Scenario: "single-map",
		ImageRef: "quay.io/numaproj/numaflow:v1.8.0", Status: StatusCreated,
	}
	if err := repo.CreateRun(ctx, params); err != nil {
		t.Fatal(err)
	}
	if _, found, err := repo.FindBenchmarkRun(ctx, params.Scenario, params.ImageRef); err != nil || !found {
		t.Fatalf("FindBenchmarkRun found=%v err=%v", found, err)
	}

	params.ID = uuid.NewString()
	if err := repo.CreateRun(ctx, params); err == nil {
		t.Fatal("expected duplicate benchmark scenario/image to fail")
	}
}
