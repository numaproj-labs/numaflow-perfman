package results

import (
	"context"
	"fmt"
)

// ReplaceValidationFailureSamples replaces all failure samples for a run atomically.
func (r *Repository) ReplaceValidationFailureSamples(ctx context.Context, runID string, samples []ValidationFailureSample) error {
	if runID == "" {
		return fmt.Errorf("run id is required")
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin failure samples tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, `DELETE FROM validation_failure_samples WHERE run_id = ?`, runID); err != nil {
		return fmt.Errorf("clear failure samples: %w", err)
	}
	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO validation_failure_samples (run_id, ordinal, kind, logical_key, expected, actual, detail)
		VALUES (?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return fmt.Errorf("prepare failure sample insert: %w", err)
	}
	defer stmt.Close()

	for i, s := range samples {
		ordinal := s.Ordinal
		if ordinal == 0 {
			ordinal = i + 1
		}
		if s.Kind == "" {
			return fmt.Errorf("failure sample kind is required at ordinal %d", ordinal)
		}
		if _, err := stmt.ExecContext(ctx, runID, ordinal, s.Kind, s.LogicalKey, s.Expected, s.Actual, s.Detail); err != nil {
			return fmt.Errorf("insert failure sample ordinal %d: %w", ordinal, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit failure samples: %w", err)
	}
	return nil
}

// LoadValidationFailureSamples returns failure samples for a run ordered by ordinal.
func (r *Repository) LoadValidationFailureSamples(ctx context.Context, runID string) ([]ValidationFailureSample, error) {
	if runID == "" {
		return nil, fmt.Errorf("run id is required")
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT ordinal, kind, logical_key, expected, actual, detail
		FROM validation_failure_samples
		WHERE run_id = ?
		ORDER BY ordinal ASC`, runID)
	if err != nil {
		return nil, fmt.Errorf("load failure samples: %w", err)
	}
	defer rows.Close()

	var out []ValidationFailureSample
	for rows.Next() {
		var s ValidationFailureSample
		if err := rows.Scan(&s.Ordinal, &s.Kind, &s.LogicalKey, &s.Expected, &s.Actual, &s.Detail); err != nil {
			return nil, fmt.Errorf("scan failure sample: %w", err)
		}
		out = append(out, s)
	}
	return out, rows.Err()
}
