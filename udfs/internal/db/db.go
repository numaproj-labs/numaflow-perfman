// Package db provides a PostgreSQL connection pool for the validation UDFs,
// configured from POSTGRES_* environment variables. Mirrors the Kotlin
// db.Database (HikariCP) helper.
package db

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// NewPool creates a pgx connection pool for PostgreSQL using POSTGRES_* env vars.
// Defaults match the Kotlin implementation: localhost:5432, user/password numaflow,
// database numaflow_validation.
func NewPool(ctx context.Context) (*pgxpool.Pool, error) {
	host := envOrDefault("POSTGRES_HOST", "localhost")
	port := envOrDefault("POSTGRES_PORT", "5432")
	user := envOrDefault("POSTGRES_USER", "numaflow")
	password := envOrDefault("POSTGRES_PASSWORD", "numaflow")
	dbName := envOrDefault("POSTGRES_DB", "numaflow_validation")

	dsn := fmt.Sprintf("postgres://%s:%s@%s:%s/%s", user, password, host, port, dbName)

	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("parsing postgres config: %w", err)
	}
	// Pool sizing mirrors the Kotlin HikariCP config (max 10, min idle 2).
	cfg.MaxConns = 10
	cfg.MinConns = 2
	cfg.MaxConnIdleTime = 30 * time.Second
	cfg.MaxConnLifetime = 30 * time.Minute

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("creating postgres pool: %w", err)
	}
	return pool, nil
}
