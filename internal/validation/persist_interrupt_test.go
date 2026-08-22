package validation

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	"numa-perfman/internal/results"
)

func TestPersistScenarioOutcomeMarksCancellationInterrupted(t *testing.T) {
	ctx := context.Background()
	repo, err := results.Open(ctx, filepath.Join(t.TempDir(), "results.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = repo.Close() })

	runID := uuid.New()
	if err := repo.CreateRun(ctx, results.CreateRunParams{
		ID:       runID.String(),
		Kind:     results.KindValidation,
		Scenario: "map",
		ImageRef: "numaflow:local",
		Status:   results.StatusCreated,
	}); err != nil {
		t.Fatal(err)
	}

	p := PersistingRunner{Results: RepositoryResults{Repository: repo}}
	if err := p.persistScenarioOutcome(ctx, runID, ScenarioResult{Scenario: "map", Phase: PhaseFailed}, nil, "", context.Canceled); err != nil {
		t.Fatalf("persistScenarioOutcome: %v", err)
	}

	run, err := repo.GetRun(ctx, runID.String())
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != results.StatusInterrupted {
		t.Fatalf("status = %q, want %q", run.Status, results.StatusInterrupted)
	}
}
