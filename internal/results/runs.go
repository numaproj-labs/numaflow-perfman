package results

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

const timeLayout = time.RFC3339Nano

func formatTime(t time.Time) string {
	return t.UTC().Format(timeLayout)
}

func parseTimePtr(s sql.NullString) (*time.Time, error) {
	if !s.Valid || s.String == "" {
		return nil, nil
	}
	t, err := time.Parse(timeLayout, s.String)
	if err != nil {
		// Fallback for shorter RFC3339 from external tools.
		t, err = time.Parse(time.RFC3339, s.String)
		if err != nil {
			return nil, err
		}
	}
	utc := t.UTC()
	return &utc, nil
}

func parseTime(s string) (time.Time, error) {
	t, err := time.Parse(timeLayout, s)
	if err != nil {
		t, err = time.Parse(time.RFC3339, s)
	}
	return t.UTC(), err
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanRunRow(row rowScanner) (Run, error) {
	var run Run
	var createdAt string
	var warmup, measureStart, measureEnd, completed sql.NullString
	var seed, eventCount sql.NullInt64
	err := row.Scan(
		&run.ID, &run.Kind, &run.Scenario, &run.ImageRef, &run.ImageDigest,
		&run.Status, &run.Namespace, &createdAt, &warmup, &measureStart, &measureEnd, &completed,
		&seed, &eventCount, &run.ManifestHash, &run.ConfigJSON, &run.EnvironmentJSON, &run.ErrorMessage,
	)
	if err != nil {
		return Run{}, err
	}
	var err2 error
	run.CreatedAt, err2 = parseTime(createdAt)
	if err2 != nil {
		return Run{}, fmt.Errorf("parse created_at: %w", err2)
	}
	run.WarmupStartedAt, err = parseTimePtr(warmup)
	if err != nil {
		return Run{}, err
	}
	run.MeasurementStartedAt, err = parseTimePtr(measureStart)
	if err != nil {
		return Run{}, err
	}
	run.MeasurementEndedAt, err = parseTimePtr(measureEnd)
	if err != nil {
		return Run{}, err
	}
	run.CompletedAt, err = parseTimePtr(completed)
	if err != nil {
		return Run{}, err
	}
	if seed.Valid {
		run.Seed = &seed.Int64
	}
	if eventCount.Valid {
		run.EventCount = &eventCount.Int64
	}
	return run, nil
}

const runSelectCols = `id, kind, scenario, image_ref, image_digest, status, namespace,
created_at, warmup_started_at, measurement_started_at, measurement_ended_at, completed_at,
seed, event_count, manifest_hash, config_json, environment_json, error_message`

// CreateRun inserts a new run row. The caller supplies the UUID id.
func (r *Repository) CreateRun(ctx context.Context, p CreateRunParams) error {
	if p.ID == "" {
		return fmt.Errorf("run id is required")
	}
	if p.Kind != KindBenchmark && p.Kind != KindValidation {
		return fmt.Errorf("invalid kind %q", p.Kind)
	}
	if p.Scenario == "" || p.ImageRef == "" {
		return fmt.Errorf("scenario and image_ref are required")
	}
	if p.Status == "" {
		p.Status = StatusCreated
	}
	configJSON, err := normalizeJSON(p.ConfigJSON, true)
	if err != nil {
		return fmt.Errorf("config_json: %w", err)
	}
	envJSON, err := normalizeJSON(p.EnvironmentJSON, true)
	if err != nil {
		return fmt.Errorf("environment_json: %w", err)
	}
	createdAt := p.CreatedAt
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
	}

	_, err = r.db.ExecContext(ctx, `
		INSERT INTO runs (
			id, kind, scenario, image_ref, image_digest, status, namespace,
			created_at, seed, event_count, manifest_hash, config_json, environment_json
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		p.ID, p.Kind, p.Scenario, p.ImageRef, p.ImageDigest, p.Status, p.Namespace,
		formatTime(createdAt), p.Seed, p.EventCount, p.ManifestHash, configJSON, envJSON,
	)
	if err != nil {
		return fmt.Errorf("insert run: %w", err)
	}
	return nil
}

// UpdateRunLifecycle updates status, lifecycle timestamps, and error message.
func (r *Repository) UpdateRunLifecycle(ctx context.Context, runID string, p UpdateRunLifecycleParams) error {
	if runID == "" {
		return fmt.Errorf("run id is required")
	}
	if p.Status == "" {
		return fmt.Errorf("status is required")
	}

	sets := []string{"status = ?"}
	args := []any{p.Status}

	if p.WarmupStartedAt != nil {
		sets = append(sets, "warmup_started_at = ?")
		args = append(args, formatTime(*p.WarmupStartedAt))
	}
	if p.MeasurementStartedAt != nil {
		sets = append(sets, "measurement_started_at = ?")
		args = append(args, formatTime(*p.MeasurementStartedAt))
	}
	if p.MeasurementEndedAt != nil {
		sets = append(sets, "measurement_ended_at = ?")
		args = append(args, formatTime(*p.MeasurementEndedAt))
	}
	if p.CompletedAt != nil {
		sets = append(sets, "completed_at = ?")
		args = append(args, formatTime(*p.CompletedAt))
	}
	if p.ErrorMessage != "" {
		sets = append(sets, "error_message = ?")
		args = append(args, p.ErrorMessage)
	}
	args = append(args, runID)

	q := fmt.Sprintf(`UPDATE runs SET %s WHERE id = ?`, strings.Join(sets, ", "))
	res, err := r.db.ExecContext(ctx, q, args...)
	if err != nil {
		return fmt.Errorf("update run lifecycle: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("run %q not found", runID)
	}
	return nil
}

// AddEvent appends a run event.
func (r *Repository) AddEvent(ctx context.Context, p AddEventParams) error {
	if p.RunID == "" {
		return fmt.Errorf("run id is required")
	}
	detail, err := normalizeJSON(p.DetailJSON, true)
	if err != nil {
		return fmt.Errorf("detail_json: %w", err)
	}
	ts := p.Timestamp
	if ts.IsZero() {
		ts = time.Now().UTC()
	}
	_, err = r.db.ExecContext(ctx, `
		INSERT INTO run_events (run_id, timestamp, level, phase, message, detail_json)
		VALUES (?, ?, ?, ?, ?, ?)`,
		p.RunID, formatTime(ts), p.Level, p.Phase, p.Message, detail,
	)
	if err != nil {
		return fmt.Errorf("insert run event: %w", err)
	}
	return nil
}

// GetRun returns one run by id.
func (r *Repository) GetRun(ctx context.Context, runID string) (Run, error) {
	row := r.db.QueryRowContext(ctx, `SELECT `+runSelectCols+` FROM runs WHERE id = ?`, runID)
	run, err := scanRunRow(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Run{}, fmt.Errorf("run %q not found", runID)
	}
	if err != nil {
		return Run{}, fmt.Errorf("get run: %w", err)
	}
	return run, nil
}

// FindBenchmarkRun returns the stored benchmark run for a scenario and image reference.
func (r *Repository) FindBenchmarkRun(ctx context.Context, scenario, imageRef string) (Run, bool, error) {
	if scenario == "" || imageRef == "" {
		return Run{}, false, fmt.Errorf("scenario and image_ref are required")
	}
	row := r.db.QueryRowContext(ctx, `
		SELECT `+runSelectCols+`
		FROM runs
		WHERE kind = ? AND scenario = ? AND image_ref = ?
		ORDER BY created_at DESC
		LIMIT 1`,
		KindBenchmark, scenario, imageRef,
	)
	run, err := scanRunRow(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Run{}, false, nil
	}
	if err != nil {
		return Run{}, false, fmt.Errorf("find benchmark run: %w", err)
	}
	return run, true, nil
}

// FindBenchmarkRunByNamespace returns the most recent benchmark run for a scenario and namespace.
func (r *Repository) FindBenchmarkRunByNamespace(ctx context.Context, scenario, namespace string) (Run, bool, error) {
	if scenario == "" || namespace == "" {
		return Run{}, false, fmt.Errorf("scenario and namespace are required")
	}
	row := r.db.QueryRowContext(ctx, `
		SELECT `+runSelectCols+`
		FROM runs
		WHERE kind = ? AND scenario = ? AND namespace = ?
		ORDER BY created_at DESC
		LIMIT 1`,
		KindBenchmark, scenario, namespace,
	)
	run, err := scanRunRow(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Run{}, false, nil
	}
	if err != nil {
		return Run{}, false, fmt.Errorf("find benchmark run by namespace: %w", err)
	}
	return run, true, nil
}

// ListRuns returns runs matching filter criteria, newest first.
func (r *Repository) ListRuns(ctx context.Context, f ListRunsFilter) ([]Run, error) {
	q := `SELECT ` + runSelectCols + ` FROM runs WHERE 1=1`
	var args []any

	if f.Kind != "" {
		q += ` AND kind = ?`
		args = append(args, f.Kind)
	}
	if f.Scenario != "" {
		q += ` AND scenario = ?`
		args = append(args, f.Scenario)
	}
	if f.ImageRef != "" {
		q += ` AND image_ref = ?`
		args = append(args, f.ImageRef)
	}
	if f.ImageDigest != "" {
		q += ` AND image_digest = ?`
		args = append(args, f.ImageDigest)
	}
	if f.Status != "" {
		q += ` AND status = ?`
		args = append(args, f.Status)
	}
	if len(f.StatusIn) > 0 {
		placeholders := make([]string, len(f.StatusIn))
		for i, st := range f.StatusIn {
			placeholders[i] = "?"
			args = append(args, st)
		}
		q += ` AND status IN (` + strings.Join(placeholders, ",") + `)`
	}
	if f.After != nil {
		q += ` AND created_at >= ?`
		args = append(args, formatTime(*f.After))
	}
	if f.Before != nil {
		q += ` AND created_at <= ?`
		args = append(args, formatTime(*f.Before))
	}
	q += ` ORDER BY created_at DESC`
	if f.Limit > 0 {
		q += ` LIMIT ?`
		args = append(args, f.Limit)
	}
	if f.Offset > 0 {
		q += ` OFFSET ?`
		args = append(args, f.Offset)
	}

	rows, err := r.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("list runs: %w", err)
	}
	defer rows.Close()

	var out []Run
	for rows.Next() {
		run, err := scanRunRow(rows)
		if err != nil {
			return nil, fmt.Errorf("scan run: %w", err)
		}
		out = append(out, run)
	}
	return out, rows.Err()
}

// DeleteRun removes a run and cascaded metric, event, and validation rows.
func (r *Repository) DeleteRun(ctx context.Context, runID string) error {
	res, err := r.db.ExecContext(ctx, `DELETE FROM runs WHERE id = ?`, runID)
	if err != nil {
		return fmt.Errorf("delete run: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("run %q not found", runID)
	}
	return nil
}
