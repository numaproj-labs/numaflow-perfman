package results

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// SaveMetrics persists metric series and points in a single transaction.
// Every name in RequiredMetrics must appear at least once in Series; otherwise the
// transaction rolls back.
func (r *Repository) SaveMetrics(ctx context.Context, p SaveMetricsParams) error {
	if p.RunID == "" {
		return fmt.Errorf("run id is required")
	}
	if len(p.Series) == 0 && len(p.RequiredMetrics) > 0 {
		return fmt.Errorf("missing required metrics: %v", p.RequiredMetrics)
	}

	present := make(map[string]struct{}, len(p.Series))
	for _, s := range p.Series {
		if s.MetricName == "" {
			return fmt.Errorf("metric_name is required")
		}
		present[s.MetricName] = struct{}{}
	}
	var missing []string
	for _, req := range p.RequiredMetrics {
		if _, ok := present[req]; !ok {
			missing = append(missing, req)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("missing required metrics: %v", missing)
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin metrics tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	for _, s := range p.Series {
		labelsJSON, err := normalizeJSON(s.LabelsJSON, true)
		if err != nil {
			return fmt.Errorf("labels_json for %q: %w", s.MetricName, err)
		}
		res, err := tx.ExecContext(ctx, `
			INSERT INTO metric_series (run_id, metric_name, display_name, unit, prometheus_query, labels_json)
			VALUES (?, ?, ?, ?, ?, ?)
			ON CONFLICT(run_id, metric_name, labels_json) DO UPDATE SET
				display_name = excluded.display_name,
				unit = excluded.unit,
				prometheus_query = excluded.prometheus_query`,
			p.RunID, s.MetricName, s.DisplayName, s.Unit, s.PrometheusQuery, labelsJSON,
		)
		if err != nil {
			return fmt.Errorf("insert metric series %q: %w", s.MetricName, err)
		}
		seriesID, err := res.LastInsertId()
		if err != nil || seriesID == 0 {
			err = tx.QueryRowContext(ctx, `
				SELECT id FROM metric_series WHERE run_id = ? AND metric_name = ? AND labels_json = ?`,
				p.RunID, s.MetricName, labelsJSON,
			).Scan(&seriesID)
			if err != nil {
				return fmt.Errorf("resolve series id for %q: %w", s.MetricName, err)
			}
		}

		if _, err := tx.ExecContext(ctx, `DELETE FROM metric_points WHERE series_id = ?`, seriesID); err != nil {
			return fmt.Errorf("clear points for series %d: %w", seriesID, err)
		}

		stmt, err := tx.PrepareContext(ctx, `
			INSERT INTO metric_points (series_id, timestamp, elapsed_milliseconds, value)
			VALUES (?, ?, ?, ?)`)
		if err != nil {
			return fmt.Errorf("prepare point insert: %w", err)
		}
		for _, pt := range s.Points {
			if pt.Timestamp.IsZero() {
				return fmt.Errorf("point timestamp required for metric %q", s.MetricName)
			}
			if _, err := stmt.ExecContext(ctx, seriesID, formatTime(pt.Timestamp), pt.ElapsedMilliseconds, pt.Value); err != nil {
				_ = stmt.Close()
				return fmt.Errorf("insert point: %w", err)
			}
		}
		_ = stmt.Close()
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit metrics: %w", err)
	}
	return nil
}

// LoadMetricSeries returns all metric series and points for a run, ordered by elapsed time.
func (r *Repository) LoadMetricSeries(ctx context.Context, runID string) ([]LoadedMetricSeries, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, run_id, metric_name, display_name, unit, prometheus_query, labels_json
		FROM metric_series WHERE run_id = ? ORDER BY metric_name, id`, runID)
	if err != nil {
		return nil, fmt.Errorf("list metric series: %w", err)
	}
	defer rows.Close()

	var series []LoadedMetricSeries
	for rows.Next() {
		var s LoadedMetricSeries
		if err := rows.Scan(&s.ID, &s.RunID, &s.MetricName, &s.DisplayName, &s.Unit, &s.PrometheusQuery, &s.LabelsJSON); err != nil {
			return nil, fmt.Errorf("scan series: %w", err)
		}
		series = append(series, s)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	for i := range series {
		points, err := r.loadPoints(ctx, series[i].ID)
		if err != nil {
			return nil, err
		}
		series[i].Points = points
	}
	return series, nil
}

func (r *Repository) loadPoints(ctx context.Context, seriesID int64) ([]MetricPoint, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT timestamp, elapsed_milliseconds, value
		FROM metric_points WHERE series_id = ? ORDER BY elapsed_milliseconds`, seriesID)
	if err != nil {
		return nil, fmt.Errorf("load points: %w", err)
	}
	defer rows.Close()

	var points []MetricPoint
	for rows.Next() {
		var ts string
		var pt MetricPoint
		if err := rows.Scan(&ts, &pt.ElapsedMilliseconds, &pt.Value); err != nil {
			return nil, err
		}
		t, err := parseTime(ts)
		if err != nil {
			return nil, fmt.Errorf("parse point timestamp: %w", err)
		}
		pt.Timestamp = t
		points = append(points, pt)
	}
	return points, rows.Err()
}

// SaveValidationResult upserts validation outcome for a run.
func (r *Repository) SaveValidationResult(ctx context.Context, v ValidationResult) error {
	if v.RunID == "" {
		return fmt.Errorf("run id is required")
	}
	detail, err := normalizeJSON(v.DetailJSON, true)
	if err != nil {
		return fmt.Errorf("detail_json: %w", err)
	}
	passed := 0
	if v.Passed {
		passed = 1
	}
	_, err = r.db.ExecContext(ctx, `
		INSERT INTO validation_results (
			run_id, passed, source_count, expected_count, logical_output_count,
			physical_delivery_count, duplicate_delivery_count, duplicate_rate, missing_count,
			unexpected_count, corrupted_count, routing_mismatch_count, child_mismatch_count,
			detail_json
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(run_id) DO UPDATE SET
			passed = excluded.passed,
			source_count = excluded.source_count,
			expected_count = excluded.expected_count,
			logical_output_count = excluded.logical_output_count,
			physical_delivery_count = excluded.physical_delivery_count,
			duplicate_delivery_count = excluded.duplicate_delivery_count,
			duplicate_rate = excluded.duplicate_rate,
			missing_count = excluded.missing_count,
			unexpected_count = excluded.unexpected_count,
			corrupted_count = excluded.corrupted_count,
			routing_mismatch_count = excluded.routing_mismatch_count,
			child_mismatch_count = excluded.child_mismatch_count,
			detail_json = excluded.detail_json`,
		v.RunID, passed, v.SourceCount, v.ExpectedCount, v.LogicalOutputCount,
		v.PhysicalDeliveryCount, v.DuplicateDeliveryCount, v.DuplicateRate, v.MissingCount,
		v.UnexpectedCount, v.CorruptedCount, v.RoutingMismatchCount, v.ChildMismatchCount, detail,
	)
	if err != nil {
		return fmt.Errorf("save validation result: %w", err)
	}
	return nil
}

// GetValidationResult returns validation outcome for a run, if present.
func (r *Repository) GetValidationResult(ctx context.Context, runID string) (ValidationResult, bool, error) {
	var v ValidationResult
	var passed int
	err := r.db.QueryRowContext(ctx, `
		SELECT run_id, passed, source_count, expected_count, logical_output_count,
			physical_delivery_count, duplicate_delivery_count, duplicate_rate, missing_count,
			unexpected_count, corrupted_count, routing_mismatch_count, child_mismatch_count,
			detail_json
		FROM validation_results WHERE run_id = ?`, runID,
	).Scan(
		&v.RunID, &passed, &v.SourceCount, &v.ExpectedCount, &v.LogicalOutputCount,
		&v.PhysicalDeliveryCount, &v.DuplicateDeliveryCount, &v.DuplicateRate, &v.MissingCount,
		&v.UnexpectedCount, &v.CorruptedCount, &v.RoutingMismatchCount, &v.ChildMismatchCount,
		&v.DetailJSON,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ValidationResult{}, false, nil
		}
		return ValidationResult{}, false, fmt.Errorf("get validation result: %w", err)
	}
	v.Passed = passed != 0
	return v, true, nil
}
