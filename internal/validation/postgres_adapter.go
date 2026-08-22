package validation

import (
	"context"
	"fmt"
	"net/url"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"numa-perfman/internal/oracle"
)

var validationDBNamePattern = regexp.MustCompile(`^validation_[a-z0-9_]+$`)

// PostgresCredentials configures host-facing Postgres access (typically port-forwarded admin DSN).
type PostgresCredentials struct {
	AdminDSN string
	User     string
	Password string
	Host     string
	Port     string
}

// CommandRunner executes external tools such as pg_dump (tests inject fakes).
type CommandRunner interface {
	Run(ctx context.Context, name string, args ...string) (stdout, stderr string, err error)
}

type execCommandRunner struct{}

func (execCommandRunner) Run(ctx context.Context, name string, args ...string) (string, string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	var outBuf, errBuf strings.Builder
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf
	err := cmd.Run()
	return outBuf.String(), errBuf.String(), err
}

// DefaultCommandRunner runs pg_dump without a shell.
var DefaultCommandRunner CommandRunner = execCommandRunner{}

// PostgresAdapter implements RunDatabase against shared validation Postgres.
type PostgresAdapter struct {
	Creds  PostgresCredentials
	Runner CommandRunner

	mu    sync.Mutex
	pools map[string]*pgxpool.Pool
}

func (a *PostgresAdapter) runner() CommandRunner {
	if a.Runner != nil {
		return a.Runner
	}
	return DefaultCommandRunner
}

func validateDBName(name string) error {
	if name == "" {
		return fmt.Errorf("database name is required")
	}
	if !validationDBNamePattern.MatchString(name) {
		return fmt.Errorf("invalid validation database name %q", name)
	}
	return nil
}

func quoteIdentifier(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

func (a *PostgresAdapter) runDSN(dbName string) (string, error) {
	if err := validateDBName(dbName); err != nil {
		return "", err
	}
	c := a.Creds
	user := c.User
	pass := c.Password
	host := c.Host
	port := c.Port
	if user == "" {
		user = "numaflow"
	}
	if pass == "" {
		pass = "numaflow"
	}
	if host == "" {
		host = "127.0.0.1"
	}
	if port == "" {
		port = "5432"
	}
	u := url.URL{
		Scheme: "postgres",
		User:   url.UserPassword(user, pass),
		Host:   fmt.Sprintf("%s:%s", host, port),
		Path:   "/" + dbName,
	}
	return u.String(), nil
}

func (a *PostgresAdapter) adminConn(ctx context.Context) (*pgx.Conn, error) {
	if strings.TrimSpace(a.Creds.AdminDSN) == "" {
		return nil, fmt.Errorf("admin DSN is required")
	}
	return pgx.Connect(ctx, a.Creds.AdminDSN)
}

func (a *PostgresAdapter) poolFor(ctx context.Context, dbName string) (*pgxpool.Pool, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.pools == nil {
		a.pools = make(map[string]*pgxpool.Pool)
	}
	if p, ok := a.pools[dbName]; ok {
		return p, nil
	}
	dsn, err := a.runDSN(dbName)
	if err != nil {
		return nil, err
	}
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, err
	}
	cfg.MaxConns = 4
	cfg.MinConns = 1
	cfg.MaxConnLifetime = 30 * time.Minute
	p, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, err
	}
	a.pools[dbName] = p
	return p, nil
}

func (a *PostgresAdapter) closePool(dbName string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.pools == nil {
		return
	}
	if p, ok := a.pools[dbName]; ok {
		p.Close()
		delete(a.pools, dbName)
	}
}

func (a *PostgresAdapter) Create(ctx context.Context, dbName string, bundle oracle.Bundle) error {
	if err := validateDBName(dbName); err != nil {
		return err
	}
	admin, err := a.adminConn(ctx)
	if err != nil {
		return WrapInfrastructure(err)
	}
	defer admin.Close(ctx)

	_, err = admin.Exec(ctx, "CREATE DATABASE "+quoteIdentifier(dbName))
	if err != nil {
		return WrapInfrastructure(fmt.Errorf("create database: %w", err))
	}

	pool, err := a.poolFor(ctx, dbName)
	if err != nil {
		_, _ = admin.Exec(ctx, "DROP DATABASE IF EXISTS "+quoteIdentifier(dbName))
		return WrapInfrastructure(err)
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		return WrapInfrastructure(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, schemaDDLForScenario(bundle.Scenario)); err != nil {
		return WrapInfrastructure(fmt.Errorf("schema: %w", err))
	}
	if err := insertBundle(ctx, tx, bundle); err != nil {
		return WrapInfrastructure(err)
	}
	eventCount := bundle.Config.EventCount
	if bundle.Scenario == oracle.ScenarioMap || bundle.Scenario == oracle.ScenarioMonoVertex {
		eventCount = int64(len(bundle.MapSources))
	} else {
		eventCount = int64(len(bundle.ReduceSources))
	}
	if _, err := tx.Exec(ctx, "UPDATE run_config SET total_events = $1, source_completed = FALSE", eventCount); err != nil {
		return WrapInfrastructure(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return WrapInfrastructure(err)
	}
	return nil
}

func insertBundle(ctx context.Context, tx pgx.Tx, bundle oracle.Bundle) error {
	switch bundle.Scenario {
	case oracle.ScenarioMap, oracle.ScenarioMonoVertex:
		return insertMapBundle(ctx, tx, bundle)
	case oracle.ScenarioReduce, oracle.ScenarioSlidingReduce:
		return insertReduceBundle(ctx, tx, bundle)
	default:
		return fmt.Errorf("unsupported scenario %q", bundle.Scenario)
	}
}

func insertMapBundle(ctx context.Context, tx pgx.Tx, bundle oracle.Bundle) error {
	for _, src := range bundle.MapSources {
		ev := src.Event
		if _, err := tx.Exec(ctx, `
			INSERT INTO source_events (
				event_id, user_id, page_id, ad_type, event_type, event_time, ip_address, route_tag
			) VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`,
			ev.EventID, ev.UserID, ev.PageID, ev.AdType, ev.EventType,
			time.UnixMilli(ev.EventTime).UTC(), ev.IpAddress, src.RouteTag,
		); err != nil {
			return err
		}
	}
	for _, exp := range bundle.Expected {
		if _, err := tx.Exec(ctx, `
			INSERT INTO expected_sink_events (
				dedup_key, event_id, payload, processed_by, child_index, total_children
			) VALUES ($1,$2,$3::jsonb,$4,$5,$6)`,
			exp.LogicalKey, exp.EventID, string(exp.Payload),
			exp.ProcessedBy, exp.ChildIndex, exp.TotalChildren,
		); err != nil {
			return err
		}
	}
	return nil
}

func insertReduceBundle(ctx context.Context, tx pgx.Tx, bundle oracle.Bundle) error {
	for _, src := range bundle.ReduceSources {
		if _, err := tx.Exec(ctx, `
			INSERT INTO source_events (event_id, reduce_key, category, amount, event_time)
			VALUES ($1,$2,$3,$4,$5)`,
			src.EventID, src.ReduceKey, src.Category, src.Amount, src.EventTime,
		); err != nil {
			return err
		}
	}
	for _, exp := range bundle.Expected {
		if _, err := tx.Exec(ctx, `
			INSERT INTO expected_sink_events (
				dedup_key, event_id, payload, processed_by, child_index, total_children
			) VALUES ($1,$2,$3::jsonb,$4,$5,$6)`,
			exp.LogicalKey, exp.EventID, string(exp.Payload),
			exp.ProcessedBy, exp.ChildIndex, exp.TotalChildren,
		); err != nil {
			return err
		}
	}
	return nil
}

func (a *PostgresAdapter) Drop(ctx context.Context, dbName string) error {
	if err := validateDBName(dbName); err != nil {
		return err
	}
	a.closePool(dbName)
	admin, err := a.adminConn(ctx)
	if err != nil {
		return WrapInfrastructure(err)
	}
	defer admin.Close(ctx)
	_, err = admin.Exec(ctx, "DROP DATABASE IF EXISTS "+quoteIdentifier(dbName))
	if err != nil {
		return WrapInfrastructure(err)
	}
	return nil
}

func (a *PostgresAdapter) Progress(ctx context.Context, dbName string) (ProgressSnapshot, error) {
	pool, err := a.poolFor(ctx, dbName)
	if err != nil {
		return ProgressSnapshot{}, WrapInfrastructure(err)
	}
	qctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	var snap ProgressSnapshot
	var total int64
	var completed bool
	if err := pool.QueryRow(qctx, "SELECT total_events, source_completed FROM run_config LIMIT 1").Scan(&total, &completed); err != nil {
		return snap, WrapInfrastructure(err)
	}
	snap.TotalEvents = total
	snap.SourceCompleted = completed
	if err := pool.QueryRow(qctx, "SELECT COUNT(*) FROM source_events WHERE ack_status = 'ACKED'").Scan(&snap.AckedEvents); err != nil {
		return snap, WrapInfrastructure(err)
	}
	if err := pool.QueryRow(qctx, "SELECT COUNT(*) FROM sink_events").Scan(&snap.SinkRows); err != nil {
		return snap, WrapInfrastructure(err)
	}
	if err := pool.QueryRow(qctx, "SELECT COUNT(*) FROM expected_sink_events").Scan(&snap.ExpectedRows); err != nil {
		return snap, WrapInfrastructure(err)
	}
	snap.At = time.Now().UTC()
	return snap, nil
}

func (a *PostgresAdapter) ListSinkDeliveries(ctx context.Context, dbName string) ([]PhysicalDelivery, error) {
	pool, err := a.poolFor(ctx, dbName)
	if err != nil {
		return nil, WrapInfrastructure(err)
	}
	qctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()

	rows, err := pool.Query(qctx, `
		SELECT dedup_key, event_id, processed_by, child_index, total_children, payload, receive_count
		FROM sink_events ORDER BY dedup_key`)
	if err != nil {
		return nil, WrapInfrastructure(err)
	}
	defer rows.Close()

	var out []PhysicalDelivery
	for rows.Next() {
		var d PhysicalDelivery
		if err := rows.Scan(&d.LogicalKey, &d.EventID, &d.ProcessedBy, &d.ChildIndex, &d.TotalChildren, &d.Payload, &d.ReceiveCount); err != nil {
			return nil, WrapInfrastructure(err)
		}
		if d.ReceiveCount == 0 {
			d.ReceiveCount = 1
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func (a *PostgresAdapter) Dump(ctx context.Context, dbName string) ([]byte, error) {
	if err := validateDBName(dbName); err != nil {
		return nil, err
	}
	dsn, err := a.runDSN(dbName)
	if err != nil {
		return nil, err
	}
	stdout, stderr, err := a.runner().Run(ctx, "pg_dump", "--no-owner", "--no-acl", "--dbname", dsn)
	if err != nil {
		return nil, fmt.Errorf("pg_dump: %w: %s", err, strings.TrimSpace(stderr))
	}
	content := []byte(stdout)
	if len(content) > MaxFailureDumpBytes {
		return nil, fmt.Errorf("pg_dump output exceeds max size %d bytes", MaxFailureDumpBytes)
	}
	return content, nil
}
