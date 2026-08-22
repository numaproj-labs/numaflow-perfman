package results

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

var (
	// ErrActiveLockHeld indicates another owner holds the lock key.
	ErrActiveLockHeld = errors.New("active lock is held")
	// ErrActiveLockNotHeld indicates the owner token does not hold the lock key.
	ErrActiveLockNotHeld = errors.New("active lock is not held by owner")
)

// AcquireActiveLock inserts a lock row when the key is free.
func (r *Repository) AcquireActiveLock(ctx context.Context, p AcquireActiveLockParams) error {
	if p.LockKey == "" {
		return fmt.Errorf("lock key is required")
	}
	if p.OwnerToken == "" {
		return fmt.Errorf("owner token is required")
	}
	startedAt := p.StartedAt
	if startedAt.IsZero() {
		startedAt = time.Now().UTC()
	}
	now := time.Now().UTC()
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO active_locks (
			lock_key, owner_token, pid, host, command, run_id, namespace, scenario,
			image_ref, image_tag, started_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		p.LockKey, p.OwnerToken, p.PID, p.Host, p.Command, p.RunID, p.Namespace, p.Scenario,
		p.ImageRef, p.ImageTag, formatTime(startedAt), formatTime(now),
	)
	if err != nil {
		if isUniqueViolation(err) {
			return ErrActiveLockHeld
		}
		return fmt.Errorf("acquire active lock: %w", err)
	}
	return nil
}

// ReadActiveLock returns the lock row for a key, if present.
func (r *Repository) ReadActiveLock(ctx context.Context, lockKey string) (ActiveLock, bool, error) {
	if lockKey == "" {
		return ActiveLock{}, false, fmt.Errorf("lock key is required")
	}
	row := r.db.QueryRowContext(ctx, `
		SELECT lock_key, owner_token, pid, host, command, run_id, namespace, scenario,
			image_ref, image_tag, started_at, updated_at
		FROM active_locks WHERE lock_key = ?`, lockKey)
	lock, err := scanActiveLock(row)
	if errors.Is(err, sql.ErrNoRows) {
		return ActiveLock{}, false, nil
	}
	if err != nil {
		return ActiveLock{}, false, fmt.Errorf("read active lock: %w", err)
	}
	return lock, true, nil
}

// UpdateActiveLock updates mutable fields for a lock owned by ownerToken.
func (r *Repository) UpdateActiveLock(ctx context.Context, lockKey, ownerToken string, p UpdateActiveLockParams) error {
	if lockKey == "" || ownerToken == "" {
		return fmt.Errorf("lock key and owner token are required")
	}
	sets := []string{"updated_at = ?"}
	args := []any{formatTime(time.Now().UTC())}
	if p.PID != nil {
		sets = append(sets, "pid = ?")
		args = append(args, *p.PID)
	}
	if p.Host != nil {
		sets = append(sets, "host = ?")
		args = append(args, *p.Host)
	}
	if p.Command != nil {
		sets = append(sets, "command = ?")
		args = append(args, *p.Command)
	}
	if p.RunID != nil {
		sets = append(sets, "run_id = ?")
		args = append(args, *p.RunID)
	}
	if p.Namespace != nil {
		sets = append(sets, "namespace = ?")
		args = append(args, *p.Namespace)
	}
	if p.Scenario != nil {
		sets = append(sets, "scenario = ?")
		args = append(args, *p.Scenario)
	}
	if p.ImageRef != nil {
		sets = append(sets, "image_ref = ?")
		args = append(args, *p.ImageRef)
	}
	if p.ImageTag != nil {
		sets = append(sets, "image_tag = ?")
		args = append(args, *p.ImageTag)
	}
	args = append(args, lockKey, ownerToken)
	q := fmt.Sprintf(`UPDATE active_locks SET %s WHERE lock_key = ? AND owner_token = ?`, strings.Join(sets, ", "))
	res, err := r.db.ExecContext(ctx, q, args...)
	if err != nil {
		return fmt.Errorf("update active lock: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrActiveLockNotHeld
	}
	return nil
}

// ReleaseActiveLock deletes a lock row when owned by ownerToken.
func (r *Repository) ReleaseActiveLock(ctx context.Context, lockKey, ownerToken string) error {
	return r.deleteActiveLock(ctx, lockKey, ownerToken)
}

// DeleteActiveLock deletes a lock row when owned by ownerToken.
func (r *Repository) DeleteActiveLock(ctx context.Context, lockKey, ownerToken string) error {
	return r.deleteActiveLock(ctx, lockKey, ownerToken)
}

func (r *Repository) deleteActiveLock(ctx context.Context, lockKey, ownerToken string) error {
	if lockKey == "" || ownerToken == "" {
		return fmt.Errorf("lock key and owner token are required")
	}
	res, err := r.db.ExecContext(ctx, `
		DELETE FROM active_locks WHERE lock_key = ? AND owner_token = ?`, lockKey, ownerToken)
	if err != nil {
		return fmt.Errorf("delete active lock: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrActiveLockNotHeld
	}
	return nil
}

// ListActiveLocks returns all active lock rows ordered by lock key.
func (r *Repository) ListActiveLocks(ctx context.Context) ([]ActiveLock, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT lock_key, owner_token, pid, host, command, run_id, namespace, scenario,
			image_ref, image_tag, started_at, updated_at
		FROM active_locks ORDER BY lock_key`)
	if err != nil {
		return nil, fmt.Errorf("list active locks: %w", err)
	}
	defer rows.Close()

	var out []ActiveLock
	for rows.Next() {
		lock, err := scanActiveLock(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, lock)
	}
	return out, rows.Err()
}

func scanActiveLock(row rowScanner) (ActiveLock, error) {
	var lock ActiveLock
	var startedAt, updatedAt string
	if err := row.Scan(
		&lock.LockKey, &lock.OwnerToken, &lock.PID, &lock.Host, &lock.Command, &lock.RunID,
		&lock.Namespace, &lock.Scenario, &lock.ImageRef, &lock.ImageTag, &startedAt, &updatedAt,
	); err != nil {
		return ActiveLock{}, err
	}
	var err error
	lock.StartedAt, err = parseTime(startedAt)
	if err != nil {
		return ActiveLock{}, fmt.Errorf("parse lock started_at: %w", err)
	}
	lock.UpdatedAt, err = parseTime(updatedAt)
	if err != nil {
		return ActiveLock{}, fmt.Errorf("parse lock updated_at: %w", err)
	}
	return lock, nil
}

func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "UNIQUE constraint failed") ||
		strings.Contains(msg, "constraint failed: active_locks.lock_key")
}
