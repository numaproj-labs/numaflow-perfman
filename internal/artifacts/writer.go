package artifacts

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"numa-perfman/internal/results"
)

const (
	// KindManifest stores resolved Kubernetes manifests.
	KindManifest = "manifest"
	// KindDiagnostic stores diagnostic text blobs (logs, pod lists, etc.).
	KindDiagnostic = "diagnostic"
)

// Writer stores opaque run artifacts in the results repository.
type Writer struct {
	repo *results.Repository
}

// NewWriter creates a repository-backed artifact writer.
func NewWriter(repo *results.Repository) (*Writer, error) {
	if repo == nil {
		return nil, fmt.Errorf("results repository is required")
	}
	return &Writer{repo: repo}, nil
}

// WriteResolvedManifest stores one resolved manifest blob.
func (w *Writer) WriteResolvedManifest(runID, filename string, content []byte) error {
	return w.put(context.Background(), runID, KindManifest, filename, "application/yaml", content)
}

// WriteTextLog stores one diagnostic text blob.
func (w *Writer) WriteTextLog(runID, name, content string) error {
	return w.put(context.Background(), runID, KindDiagnostic, name, "text/plain", []byte(content))
}

// GetArtifact returns one stored artifact blob for tests and inspection.
func (w *Writer) GetArtifact(ctx context.Context, runID, kind, name string) ([]byte, error) {
	if err := validateRunID(runID); err != nil {
		return nil, err
	}
	if err := validateArtifactName(name); err != nil {
		return nil, err
	}
	art, err := w.repo.GetRunArtifact(ctx, runID, kind, name)
	if err != nil {
		return nil, err
	}
	return art.Content, nil
}

func (w *Writer) put(ctx context.Context, runID, kind, name, mediaType string, content []byte) error {
	if err := validateRunID(runID); err != nil {
		return err
	}
	if err := validateArtifactName(name); err != nil {
		return err
	}
	if content == nil {
		content = []byte{}
	}
	_, err := w.repo.PutRunArtifact(ctx, results.PutRunArtifactParams{
		RunID: runID, Kind: kind, Name: name, MediaType: mediaType, Content: content,
	})
	return err
}

func validateRunID(runID string) error {
	if runID == "" {
		return fmt.Errorf("run id is required")
	}
	if strings.Contains(runID, "..") || strings.ContainsAny(runID, `/\`) {
		return fmt.Errorf("invalid run id %q", runID)
	}
	if _, err := uuid.Parse(runID); err != nil {
		return fmt.Errorf("run id must be a UUID: %w", err)
	}
	return nil
}

func validateArtifactName(name string) error {
	if name == "" || name == "." || name == ".." {
		return fmt.Errorf("invalid artifact name %q", name)
	}
	if strings.Contains(name, "..") || strings.ContainsAny(name, `/\`) {
		return fmt.Errorf("invalid artifact name %q", name)
	}
	if strings.Contains(name, "/") || strings.Contains(name, `\`) {
		return fmt.Errorf("artifact name must not contain path separators: %q", name)
	}
	return nil
}
