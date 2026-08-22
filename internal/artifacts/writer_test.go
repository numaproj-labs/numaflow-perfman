package artifacts

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	"numa-perfman/internal/results"
)

func openTestRepo(t *testing.T) *results.Repository {
	t.Helper()
	ctx := context.Background()
	repo, err := results.Open(ctx, filepath.Join(t.TempDir(), "perfman.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = repo.Close() })
	return repo
}

func createTestRun(t *testing.T, repo *results.Repository) string {
	t.Helper()
	runID := uuid.NewString()
	if err := repo.CreateRun(context.Background(), results.CreateRunParams{
		ID: runID, Kind: results.KindBenchmark, Scenario: "single-map",
		ImageRef: "quay.io/numaproj/numaflow:v1.8.0", Status: results.StatusCreated,
	}); err != nil {
		t.Fatal(err)
	}
	return runID
}

func TestWriterManifestAndDiagnosticBlobs(t *testing.T) {
	repo := openTestRepo(t)
	w, err := NewWriter(repo)
	if err != nil {
		t.Fatal(err)
	}

	runID := createTestRun(t, repo)
	manifest := []byte("apiVersion: v1\nkind: Pipeline\n")
	if err := w.WriteResolvedManifest(runID, "pipeline.yaml", manifest); err != nil {
		t.Fatal(err)
	}
	got, err := w.GetArtifact(context.Background(), runID, KindManifest, "pipeline.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(manifest) {
		t.Fatalf("manifest = %q", got)
	}

	if err := w.WriteTextLog(runID, "pods.txt", "pod-a\n"); err != nil {
		t.Fatal(err)
	}
	diag, err := w.GetArtifact(context.Background(), runID, KindDiagnostic, "pods.txt")
	if err != nil {
		t.Fatal(err)
	}
	if string(diag) != "pod-a\n" {
		t.Fatalf("diagnostic = %q", diag)
	}

	badIDs := []string{"../escape", uuid.NewString() + "/../x", ".."}
	for _, id := range badIDs {
		if err := w.WriteTextLog(id, "diagnostic.txt", "x"); err == nil {
			t.Fatalf("expected error for run id %q", id)
		}
	}

	if err := w.WriteTextLog(runID, "../oops.txt", "x"); err == nil {
		t.Fatal("expected path escape error")
	}
	if err := w.WriteResolvedManifest(runID, "../evil.yaml", nil); err == nil {
		t.Fatal("expected manifest name error")
	}

	replaced := []byte("apiVersion: v1\nkind: Pipeline\nmetadata:\n  name: replaced\n")
	if err := w.WriteResolvedManifest(runID, "pipeline.yaml", replaced); err != nil {
		t.Fatal(err)
	}
	got2, err := w.GetArtifact(context.Background(), runID, KindManifest, "pipeline.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if string(got2) != string(replaced) {
		t.Fatal("manifest upsert did not replace content")
	}
}

func TestValidateRunIDRequiresUUID(t *testing.T) {
	if err := validateRunID("not-a-uuid"); err == nil {
		t.Fatal("expected error")
	}
}
