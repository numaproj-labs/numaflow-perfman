package results

import (
	"context"
	"database/sql"
	_ "embed"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

//go:embed schema.sql
var schemaSQL string

// Repository persists run-centric benchmark and validation results in SQLite.
type Repository struct {
	db     *sql.DB
	dbPath string
}

// Open opens or creates a results database with the current schema.
// Existing result stores are unsupported and must be deleted before use.
func Open(ctx context.Context, dbPath string) (*Repository, error) {
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil && filepath.Dir(dbPath) != "." {
		return nil, fmt.Errorf("create db directory: %w", err)
	}

	dsn := fmt.Sprintf(
		"file:%s?_pragma=foreign_keys(1)&_pragma=journal_mode(MEMORY)&_pragma=temp_store(MEMORY)&_pragma=auto_vacuum(INCREMENTAL)&_pragma=busy_timeout(5000)",
		dbPath,
	)
	sqldb, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	sqldb.SetMaxOpenConns(1)
	sqldb.SetMaxIdleConns(1)
	if err := sqldb.PingContext(ctx); err != nil {
		_ = sqldb.Close()
		return nil, fmt.Errorf("ping sqlite: %w", err)
	}

	repo := &Repository{db: sqldb, dbPath: dbPath}
	if err := repo.initializeSchema(ctx); err != nil {
		_ = sqldb.Close()
		return nil, err
	}
	return repo, nil
}

// Close closes the underlying database handle.
func (r *Repository) Close() error {
	if r == nil || r.db == nil {
		return nil
	}
	return r.db.Close()
}

// DBPath returns the filesystem path passed to Open.
func (r *Repository) DBPath() string {
	return r.dbPath
}

func (r *Repository) initializeSchema(ctx context.Context) error {
	if _, err := r.db.ExecContext(ctx, schemaSQL); err != nil {
		return fmt.Errorf("initialize results schema: %w", err)
	}
	return nil
}
