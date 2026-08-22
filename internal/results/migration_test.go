package results

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

func TestOpenPreservesLegacyTables(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "legacy.db")

	const legacySchema = `
CREATE TABLE benchmark_specs (
    id INTEGER PRIMARY KEY,
    name TEXT NOT NULL UNIQUE
);
CREATE TABLE benchmark_runs (
    id INTEGER PRIMARY KEY,
    image_tag TEXT NOT NULL,
    benchmark_id INTEGER NOT NULL
);`

	dsn := fmt.Sprintf("file:%s?_pragma=foreign_keys(1)", dbPath)
	sqldb, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sqldb.ExecContext(ctx, legacySchema); err != nil {
		t.Fatal(err)
	}
	if err := sqldb.Close(); err != nil {
		t.Fatal(err)
	}

	repo, err := Open(ctx, dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = repo.Close() })

	var n int
	if err := repo.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM benchmark_specs`).Scan(&n); err != nil {
		t.Fatalf("legacy table missing: %v", err)
	}
	if err := repo.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM runs`).Scan(&n); err != nil {
		t.Fatal(err)
	}
}
