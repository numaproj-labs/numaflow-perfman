package results

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// SaveReport persists a report payload, metadata, and linked runs in one transaction.
func (r *Repository) SaveReport(ctx context.Context, p SaveReportParams) (StoredReport, error) {
	if p.Kind != ReportKindComparison && p.Kind != ReportKindSingle {
		return StoredReport{}, fmt.Errorf("invalid report kind %q", p.Kind)
	}
	if len(p.Payload) == 0 {
		return StoredReport{}, fmt.Errorf("report payload is required")
	}
	id := p.ID
	if id == "" {
		id = uuid.NewString()
	}
	generatedAt := p.GeneratedAt
	if generatedAt.IsZero() {
		generatedAt = time.Now().UTC()
	}
	selectionJSON, err := normalizeJSON(p.SelectionJSON, true)
	if err != nil {
		return StoredReport{}, fmt.Errorf("selection_json: %w", err)
	}
	sizeBytes, sha256Hex := blobStats(p.Payload)

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return StoredReport{}, fmt.Errorf("begin report tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO reports (
			id, kind, scenario, generated_at, selection_json, payload, size_bytes, sha256,
			baseline_image_ref, candidate_image_ref
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, p.Kind, p.Scenario, formatTime(generatedAt), selectionJSON, p.Payload, sizeBytes, sha256Hex,
		p.BaselineImageRef, p.CandidateImageRef,
	); err != nil {
		return StoredReport{}, fmt.Errorf("insert report: %w", err)
	}
	for _, ref := range p.RunRefs {
		if ref.RunID == "" {
			return StoredReport{}, fmt.Errorf("report run ref run_id is required")
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO report_runs (report_id, run_id, grp) VALUES (?, ?, ?)`,
			id, ref.RunID, ref.Group,
		); err != nil {
			return StoredReport{}, fmt.Errorf("insert report run ref: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return StoredReport{}, fmt.Errorf("commit report: %w", err)
	}
	return StoredReport{
		ID:                id,
		Kind:              p.Kind,
		Scenario:          p.Scenario,
		GeneratedAt:       generatedAt,
		SelectionJSON:     selectionJSON,
		Payload:           append([]byte(nil), p.Payload...),
		SizeBytes:         sizeBytes,
		SHA256:            sha256Hex,
		BaselineImageRef:  p.BaselineImageRef,
		CandidateImageRef: p.CandidateImageRef,
		RunRefs:           append([]ReportRunRef(nil), p.RunRefs...),
	}, nil
}

// ListReports returns report summaries matching the filter, newest first.
func (r *Repository) ListReports(ctx context.Context, f ListReportsFilter) ([]ReportSummary, error) {
	q := `SELECT id, kind, scenario, generated_at, size_bytes, sha256,
		baseline_image_ref, candidate_image_ref
		FROM reports WHERE 1=1`
	var args []any
	if f.Kind != "" {
		q += ` AND kind = ?`
		args = append(args, f.Kind)
	}
	if f.Scenario != "" {
		q += ` AND scenario = ?`
		args = append(args, f.Scenario)
	}
	q += ` ORDER BY generated_at DESC`
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
		return nil, fmt.Errorf("list reports: %w", err)
	}
	defer rows.Close()

	var out []ReportSummary
	for rows.Next() {
		var summary ReportSummary
		var generatedAt string
		if err := rows.Scan(
			&summary.ID, &summary.Kind, &summary.Scenario, &generatedAt, &summary.SizeBytes, &summary.SHA256,
			&summary.BaselineImageRef, &summary.CandidateImageRef,
		); err != nil {
			return nil, fmt.Errorf("scan report summary: %w", err)
		}
		t, err := parseTime(generatedAt)
		if err != nil {
			return nil, fmt.Errorf("parse report generated_at: %w", err)
		}
		summary.GeneratedAt = t
		out = append(out, summary)
	}
	return out, rows.Err()
}

// GetReport returns one report including its payload and linked runs.
func (r *Repository) GetReport(ctx context.Context, id string) (StoredReport, error) {
	if id == "" {
		return StoredReport{}, fmt.Errorf("report id is required")
	}
	var rep StoredReport
	var generatedAt string
	err := r.db.QueryRowContext(ctx, `
		SELECT id, kind, scenario, generated_at, selection_json, payload, size_bytes, sha256,
			baseline_image_ref, candidate_image_ref
		FROM reports WHERE id = ?`, id,
	).Scan(
		&rep.ID, &rep.Kind, &rep.Scenario, &generatedAt, &rep.SelectionJSON, &rep.Payload, &rep.SizeBytes, &rep.SHA256,
		&rep.BaselineImageRef, &rep.CandidateImageRef,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return StoredReport{}, fmt.Errorf("report %q not found", id)
	}
	if err != nil {
		return StoredReport{}, fmt.Errorf("get report: %w", err)
	}
	t, err := parseTime(generatedAt)
	if err != nil {
		return StoredReport{}, fmt.Errorf("parse report generated_at: %w", err)
	}
	rep.GeneratedAt = t

	rows, err := r.db.QueryContext(ctx, `
		SELECT run_id, grp FROM report_runs WHERE report_id = ? ORDER BY run_id`, id)
	if err != nil {
		return StoredReport{}, fmt.Errorf("list report runs: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var ref ReportRunRef
		if err := rows.Scan(&ref.RunID, &ref.Group); err != nil {
			return StoredReport{}, fmt.Errorf("scan report run ref: %w", err)
		}
		rep.RunRefs = append(rep.RunRefs, ref)
	}
	if err := rows.Err(); err != nil {
		return StoredReport{}, err
	}
	return rep, nil
}
