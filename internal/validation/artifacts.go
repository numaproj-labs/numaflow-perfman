package validation

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"numa-perfman/internal/artifacts"
	"numa-perfman/internal/results"
)

const (
	artifactKindDatabaseDump = "database_dump"
	failureDumpArtifactName  = "failure-db.sql"
	failureDumpMediaType     = "application/sql"
	// MaxFailureDumpBytes bounds pg_dump capture before persisting to SQLite.
	MaxFailureDumpBytes = 64 << 20 // 64 MiB
)

// FailureDumpStore persists a failed scenario database dump.
type FailureDumpStore interface {
	PutFailureDump(ctx context.Context, runID uuid.UUID, content []byte) error
}

// ArtifactsWriter persists run-scoped blobs through the results repository.
type ArtifactsWriter interface {
	WriteResolvedManifest(runID, filename string, content []byte) error
	WriteTextLog(runID, name, content string) error
	WriteDatabaseDump(runID string, content []byte) error
}

// RepositoryArtifacts stores artifacts via PutRunArtifact.
type RepositoryArtifacts struct {
	Repo *results.Repository
}

// ArtifactsFromRepository adapts a results repository for validation artifact persistence.
func ArtifactsFromRepository(repo *results.Repository) ArtifactsWriter {
	if repo == nil {
		return nil
	}
	return RepositoryArtifacts{Repo: repo}
}

func (a RepositoryArtifacts) put(runID, kind, name, mediaType string, content []byte) error {
	if a.Repo == nil {
		return fmt.Errorf("results repository is required")
	}
	_, err := a.Repo.PutRunArtifact(context.Background(), results.PutRunArtifactParams{
		RunID: runID, Kind: kind, Name: name, MediaType: mediaType, Content: content,
	})
	return err
}

func (a RepositoryArtifacts) WriteResolvedManifest(runID, filename string, content []byte) error {
	return a.put(runID, artifacts.KindManifest, filename, "application/yaml", content)
}

func (a RepositoryArtifacts) WriteTextLog(runID, name, content string) error {
	return a.put(runID, artifacts.KindDiagnostic, name, "text/plain", []byte(content))
}

func (a RepositoryArtifacts) WriteDatabaseDump(runID string, content []byte) error {
	if len(content) > MaxFailureDumpBytes {
		return fmt.Errorf("database dump exceeds max size %d bytes", MaxFailureDumpBytes)
	}
	return a.put(runID, artifactKindDatabaseDump, failureDumpArtifactName, failureDumpMediaType, content)
}

type artifactsWriterAdapter struct {
	w    *artifacts.Writer
	repo *results.Repository
}

// ArtifactsFromWriter adapts *artifacts.Writer for validation persistence.
// repo is required for database dump artifacts because the writer exposes no dump helper.
func ArtifactsFromWriter(w *artifacts.Writer, repo *results.Repository) ArtifactsWriter {
	if w == nil {
		return nil
	}
	return artifactsWriterAdapter{w: w, repo: repo}
}

func (a artifactsWriterAdapter) WriteResolvedManifest(runID, filename string, content []byte) error {
	return a.w.WriteResolvedManifest(runID, filename, content)
}

func (a artifactsWriterAdapter) WriteTextLog(runID, name, content string) error {
	return a.w.WriteTextLog(runID, name, content)
}

func (a artifactsWriterAdapter) WriteDatabaseDump(runID string, content []byte) error {
	if a.repo == nil {
		return fmt.Errorf("results repository is required for database dump artifacts")
	}
	return RepositoryArtifacts{Repo: a.repo}.WriteDatabaseDump(runID, content)
}

type artifactsFailureDumpStore struct {
	w ArtifactsWriter
}

func (s artifactsFailureDumpStore) PutFailureDump(_ context.Context, runID uuid.UUID, content []byte) error {
	if s.w == nil || len(content) == 0 {
		return nil
	}
	return s.w.WriteDatabaseDump(runID.String(), content)
}

func newArtifactsFailureDumpStore(w ArtifactsWriter) FailureDumpStore {
	if w == nil {
		return nil
	}
	return artifactsFailureDumpStore{w: w}
}
