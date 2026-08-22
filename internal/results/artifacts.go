package results

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// PutRunArtifact stores or replaces one run artifact blob.
func (r *Repository) PutRunArtifact(ctx context.Context, p PutRunArtifactParams) (RunArtifact, error) {
	if p.RunID == "" {
		return RunArtifact{}, fmt.Errorf("run id is required")
	}
	if p.Kind == "" || p.Name == "" {
		return RunArtifact{}, fmt.Errorf("kind and name are required")
	}
	if p.Content == nil {
		p.Content = []byte{}
	}
	mediaType := p.MediaType
	if mediaType == "" {
		mediaType = "application/octet-stream"
	}
	sizeBytes, sha256Hex := blobStats(p.Content)
	createdAt := time.Now().UTC()

	_, err := r.db.ExecContext(ctx, `
		INSERT INTO run_artifacts (run_id, kind, name, media_type, content, size_bytes, sha256, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(run_id, kind, name) DO UPDATE SET
			media_type = excluded.media_type,
			content = excluded.content,
			size_bytes = excluded.size_bytes,
			sha256 = excluded.sha256,
			created_at = excluded.created_at`,
		p.RunID, p.Kind, p.Name, mediaType, p.Content, sizeBytes, sha256Hex, formatTime(createdAt),
	)
	if err != nil {
		return RunArtifact{}, fmt.Errorf("put run artifact: %w", err)
	}
	var id int64
	err = r.db.QueryRowContext(ctx, `
		SELECT id FROM run_artifacts WHERE run_id = ? AND kind = ? AND name = ?`,
		p.RunID, p.Kind, p.Name,
	).Scan(&id)
	if err != nil {
		return RunArtifact{}, fmt.Errorf("resolve run artifact id: %w", err)
	}
	return RunArtifact{
		ID:        id,
		RunID:     p.RunID,
		Kind:      p.Kind,
		Name:      p.Name,
		MediaType: mediaType,
		Content:   append([]byte(nil), p.Content...),
		SizeBytes: sizeBytes,
		SHA256:    sha256Hex,
		CreatedAt: createdAt,
	}, nil
}

// ListRunArtifacts returns artifact metadata for a run, ordered by kind and name.
func (r *Repository) ListRunArtifacts(ctx context.Context, runID string) ([]RunArtifactMeta, error) {
	if runID == "" {
		return nil, fmt.Errorf("run id is required")
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, run_id, kind, name, media_type, size_bytes, sha256, created_at
		FROM run_artifacts WHERE run_id = ? ORDER BY kind, name`, runID)
	if err != nil {
		return nil, fmt.Errorf("list run artifacts: %w", err)
	}
	defer rows.Close()

	var out []RunArtifactMeta
	for rows.Next() {
		meta, err := scanRunArtifactMeta(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, meta)
	}
	return out, rows.Err()
}

// GetRunArtifact returns one run artifact including its content blob.
func (r *Repository) GetRunArtifact(ctx context.Context, runID, kind, name string) (RunArtifact, error) {
	if runID == "" || kind == "" || name == "" {
		return RunArtifact{}, fmt.Errorf("run id, kind, and name are required")
	}
	row := r.db.QueryRowContext(ctx, `
		SELECT id, run_id, kind, name, media_type, content, size_bytes, sha256, created_at
		FROM run_artifacts WHERE run_id = ? AND kind = ? AND name = ?`, runID, kind, name)
	art, err := scanRunArtifact(row)
	if errors.Is(err, sql.ErrNoRows) {
		return RunArtifact{}, fmt.Errorf("run artifact %q/%q for run %q not found", kind, name, runID)
	}
	if err != nil {
		return RunArtifact{}, fmt.Errorf("get run artifact: %w", err)
	}
	return art, nil
}

func scanRunArtifactMeta(row rowScanner) (RunArtifactMeta, error) {
	var meta RunArtifactMeta
	var createdAt string
	if err := row.Scan(&meta.ID, &meta.RunID, &meta.Kind, &meta.Name, &meta.MediaType, &meta.SizeBytes, &meta.SHA256, &createdAt); err != nil {
		return RunArtifactMeta{}, fmt.Errorf("scan run artifact meta: %w", err)
	}
	t, err := parseTime(createdAt)
	if err != nil {
		return RunArtifactMeta{}, fmt.Errorf("parse artifact created_at: %w", err)
	}
	meta.CreatedAt = t
	return meta, nil
}

func scanRunArtifact(row rowScanner) (RunArtifact, error) {
	var art RunArtifact
	var createdAt string
	if err := row.Scan(&art.ID, &art.RunID, &art.Kind, &art.Name, &art.MediaType, &art.Content, &art.SizeBytes, &art.SHA256, &createdAt); err != nil {
		return RunArtifact{}, err
	}
	t, err := parseTime(createdAt)
	if err != nil {
		return RunArtifact{}, fmt.Errorf("parse artifact created_at: %w", err)
	}
	art.CreatedAt = t
	return art, nil
}
