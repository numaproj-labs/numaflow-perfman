// Package validator implements the SQL-based end-to-end validator. It compares the
// pre-computed expected_sink_events against the actual sink_events entirely in the
// database (JOINs + counts), so it scales to millions of events without loading them
// into memory. It writes a validation_results row and the process exits 0 (PASS) or
// 1 (FAIL). Mirrors the Kotlin Validator.
package validator

import (
	"context"
	"encoding/json"
	"log"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Result holds the outcome of a validation run.
type Result struct {
	TotalSourceEvents       int64
	TotalExpectedSinkEvents int64
	TotalActualSinkEvents   int64
	MissingCount            int64
	CorruptedCount          int64
	ExtraCount              int64
	Status                  string
	SampleMissingKeys       []string
	SampleCorruptedKeys     []string
	SampleExtraKeys         []string
}

// Validator runs the database-side validation queries.
type Validator struct {
	pool        *pgxpool.Pool
	sampleLimit int
}

// New constructs a Validator with the given sample limit (max discrepancies sampled).
func New(pool *pgxpool.Pool, sampleLimit int) *Validator {
	return &Validator{pool: pool, sampleLimit: sampleLimit}
}

// corruptedFilter enforces the same canonical full-output contract as the host
// validator, including routing and fan-out metadata.
const corruptedFilter = `(
	s.payload IS DISTINCT FROM e.payload
	OR s.processed_by IS DISTINCT FROM e.processed_by
	OR s.child_index IS DISTINCT FROM e.child_index
	OR s.total_children IS DISTINCT FROM e.total_children
)`

// Validate runs all checks, writes the result row, and returns the Result.
func (v *Validator) Validate(ctx context.Context) (Result, error) {
	var r Result

	if err := v.pool.QueryRow(ctx, `
		SELECT
			(SELECT COUNT(*) FROM source_events),
			(SELECT COUNT(*) FROM expected_sink_events),
			(SELECT COUNT(*) FROM sink_events)`).
		Scan(&r.TotalSourceEvents, &r.TotalExpectedSinkEvents, &r.TotalActualSinkEvents); err != nil {
		return r, err
	}

	if err := v.pool.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM expected_sink_events e
		LEFT JOIN sink_events s ON e.dedup_key = s.dedup_key
		WHERE s.dedup_key IS NULL`).Scan(&r.MissingCount); err != nil {
		return r, err
	}

	if err := v.pool.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM expected_sink_events e
		INNER JOIN sink_events s ON e.dedup_key = s.dedup_key
		WHERE `+corruptedFilter).Scan(&r.CorruptedCount); err != nil {
		return r, err
	}

	if err := v.pool.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM sink_events s
		LEFT JOIN expected_sink_events e ON s.dedup_key = e.dedup_key
		WHERE e.dedup_key IS NULL`).Scan(&r.ExtraCount); err != nil {
		return r, err
	}

	var err error
	if r.SampleMissingKeys, err = v.queryKeys(ctx, `
		SELECT e.dedup_key
		FROM expected_sink_events e
		LEFT JOIN sink_events s ON e.dedup_key = s.dedup_key
		WHERE s.dedup_key IS NULL
		LIMIT $1`); err != nil {
		return r, err
	}
	if r.SampleCorruptedKeys, err = v.queryKeys(ctx, `
		SELECT e.dedup_key
		FROM expected_sink_events e
		INNER JOIN sink_events s ON e.dedup_key = s.dedup_key
		WHERE `+corruptedFilter+`
		LIMIT $1`); err != nil {
		return r, err
	}
	if r.SampleExtraKeys, err = v.queryKeys(ctx, `
		SELECT s.dedup_key
		FROM sink_events s
		LEFT JOIN expected_sink_events e ON s.dedup_key = e.dedup_key
		WHERE e.dedup_key IS NULL
		LIMIT $1`); err != nil {
		return r, err
	}

	if r.MissingCount == 0 && r.CorruptedCount == 0 && r.ExtraCount == 0 {
		r.Status = "PASS"
	} else {
		r.Status = "FAIL"
	}

	if err := v.writeResults(ctx, r); err != nil {
		return r, err
	}

	log.Printf("Validation %s: %d source, %d expected, %d actual, %d missing, %d corrupted, %d extra",
		r.Status, r.TotalSourceEvents, r.TotalExpectedSinkEvents, r.TotalActualSinkEvents,
		r.MissingCount, r.CorruptedCount, r.ExtraCount)

	return r, nil
}

func (v *Validator) queryKeys(ctx context.Context, sql string) ([]string, error) {
	rows, err := v.pool.Query(ctx, sql, v.sampleLimit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var keys []string
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			return nil, err
		}
		keys = append(keys, k)
	}
	return keys, rows.Err()
}

func (v *Validator) writeResults(ctx context.Context, r Result) error {
	missingJSON, _ := json.Marshal(r.SampleMissingKeys)
	corruptedJSON, _ := json.Marshal(r.SampleCorruptedKeys)
	detailsJSON, _ := json.Marshal(map[string]any{
		"extraCount":      r.ExtraCount,
		"sampleExtraKeys": r.SampleExtraKeys,
	})

	_, err := v.pool.Exec(ctx, `
		INSERT INTO validation_results
		(total_source_events, total_expected_sink_events, total_actual_sink_events,
		 missing_count, corrupted_count, status, missing_keys, corrupted_keys, details)
		VALUES ($1, $2, $3, $4, $5, $6, $7::jsonb, $8::jsonb, $9::jsonb)`,
		r.TotalSourceEvents, r.TotalExpectedSinkEvents, r.TotalActualSinkEvents,
		r.MissingCount, r.CorruptedCount, r.Status,
		string(missingJSON), string(corruptedJSON), string(detailsJSON))
	return err
}
