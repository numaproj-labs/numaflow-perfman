package results

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

const legacyImportTableDDL = `
CREATE TABLE IF NOT EXISTS legacy_benchmark_run_imports (
    legacy_run_id INTEGER PRIMARY KEY,
    run_id        TEXT NOT NULL UNIQUE,
    imported_at   TEXT NOT NULL
);`

// importLegacyBenchmarkRuns copies legacy benchmark_runs rows into runs when the old table exists.
// Each legacy row becomes one synthetic benchmark run UUID; legacy tables are not modified.
func (r *Repository) importLegacyBenchmarkRuns(ctx context.Context) error {
	if !r.tableExists(ctx, "benchmark_runs") {
		return nil
	}
	if _, err := r.db.ExecContext(ctx, legacyImportTableDDL); err != nil {
		return fmt.Errorf("ensure legacy import table: %w", err)
	}

	hasSpecs := r.tableExists(ctx, "benchmark_specs")
	cols, err := r.tableColumns(ctx, "benchmark_runs")
	if err != nil {
		return fmt.Errorf("inspect benchmark_runs columns: %w", err)
	}
	rows, err := r.queryLegacyBenchmarkRuns(ctx, hasSpecs, cols)
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var legacyID int64
		var imageTag, scenario string
		var commitTime, startTime, endTime sql.NullString
		if err := rows.Scan(&legacyID, &imageTag, &scenario, &commitTime, &startTime, &endTime); err != nil {
			return fmt.Errorf("scan legacy benchmark run: %w", err)
		}
		runID := legacyBenchmarkRunUUID(legacyID)
		var existing int
		if err := r.db.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM legacy_benchmark_run_imports WHERE legacy_run_id = ?`, legacyID,
		).Scan(&existing); err != nil {
			return fmt.Errorf("check legacy import marker: %w", err)
		}
		if existing > 0 {
			continue
		}
		var runExists int
		if err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM runs WHERE id = ?`, runID).Scan(&runExists); err != nil {
			return fmt.Errorf("check imported run: %w", err)
		}
		if runExists > 0 {
			if _, err := r.db.ExecContext(ctx,
				`INSERT OR IGNORE INTO legacy_benchmark_run_imports (legacy_run_id, run_id, imported_at) VALUES (?, ?, ?)`,
				legacyID, runID, time.Now().UTC().Format(timeLayout),
			); err != nil {
				return fmt.Errorf("record legacy import marker: %w", err)
			}
			continue
		}

		createdAt := coalesceLegacyTime(commitTime, startTime)
		completedAt := parseLegacyTimePtr(endTime)
		if scenario == "" {
			scenario = "legacy-import"
		}
		if err := r.CreateRun(ctx, CreateRunParams{
			ID:        runID,
			Kind:      KindBenchmark,
			Scenario:  scenario,
			ImageRef:  imageTag,
			Status:    StatusCompleted,
			CreatedAt: createdAt,
		}); err != nil {
			return fmt.Errorf("import legacy run %d: %w", legacyID, err)
		}
		lifecycle := UpdateRunLifecycleParams{Status: StatusCompleted}
		if completedAt != nil {
			lifecycle.CompletedAt = completedAt
		}
		if err := r.UpdateRunLifecycle(ctx, runID, lifecycle); err != nil {
			return fmt.Errorf("finalize imported run %d: %w", legacyID, err)
		}
		if _, err := r.db.ExecContext(ctx,
			`INSERT INTO legacy_benchmark_run_imports (legacy_run_id, run_id, imported_at) VALUES (?, ?, ?)`,
			legacyID, runID, time.Now().UTC().Format(timeLayout),
		); err != nil {
			return fmt.Errorf("record legacy import: %w", err)
		}
	}
	return rows.Err()
}

func (r *Repository) queryLegacyBenchmarkRuns(ctx context.Context, hasSpecs bool, cols map[string]bool) (*sql.Rows, error) {
	commitExpr := "NULL"
	if cols["commit_time"] {
		commitExpr = "br.commit_time"
	}
	startExpr := "NULL"
	if cols["start_time"] {
		startExpr = "br.start_time"
	}
	endExpr := "NULL"
	if cols["end_time"] {
		endExpr = "br.end_time"
	}
	if hasSpecs {
		specCols, err := r.tableColumns(ctx, "benchmark_specs")
		if err != nil {
			return nil, err
		}
		scenarioExpr := "''"
		switch {
		case specCols["benchmark_name"] && specCols["pipeline_name"]:
			scenarioExpr = "COALESCE(bs.benchmark_name, bs.pipeline_name, '')"
		case specCols["benchmark_name"]:
			scenarioExpr = "COALESCE(bs.benchmark_name, '')"
		case specCols["name"] && specCols["pipeline_name"]:
			scenarioExpr = "COALESCE(bs.name, bs.pipeline_name, '')"
		case specCols["name"]:
			scenarioExpr = "COALESCE(bs.name, '')"
		case specCols["pipeline_name"]:
			scenarioExpr = "bs.pipeline_name"
		}
		return r.db.QueryContext(ctx, fmt.Sprintf(`
			SELECT br.id, br.image_tag, %s, %s, %s, %s
			FROM benchmark_runs br
			LEFT JOIN benchmark_specs bs ON bs.id = br.benchmark_id
			ORDER BY br.id ASC`, scenarioExpr, commitExpr, startExpr, endExpr))
	}
	return r.db.QueryContext(ctx, fmt.Sprintf(`
		SELECT id, image_tag, '', %s, %s, %s
		FROM benchmark_runs ORDER BY id ASC`, commitExpr, startExpr, endExpr))
}

func (r *Repository) tableColumns(ctx context.Context, table string) (map[string]bool, error) {
	rows, err := r.db.QueryContext(ctx, fmt.Sprintf("PRAGMA table_info(%s)", table))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var cid int
		var name, ctype string
		var notnull, pk int
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			return nil, err
		}
		out[name] = true
	}
	return out, rows.Err()
}

func (r *Repository) tableExists(ctx context.Context, name string) bool {
	var n int
	err := r.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?`, name,
	).Scan(&n)
	return err == nil && n > 0
}

func legacyBenchmarkRunUUID(legacyID int64) string {
	return uuid.NewSHA1(uuid.NameSpaceOID, []byte(fmt.Sprintf("legacy-benchmark-run:%d", legacyID))).String()
}

func coalesceLegacyTime(values ...sql.NullString) time.Time {
	for _, v := range values {
		if t := parseLegacyTimePtr(v); t != nil {
			return *t
		}
	}
	return time.Now().UTC()
}

func parseLegacyTimePtr(v sql.NullString) *time.Time {
	if !v.Valid || strings.TrimSpace(v.String) == "" {
		return nil
	}
	for _, layout := range []string{timeLayout, time.RFC3339, "2006-01-02 15:04:05"} {
		if t, err := time.Parse(layout, v.String); err == nil {
			utc := t.UTC()
			return &utc
		}
	}
	return nil
}
