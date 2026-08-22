package benchmark

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"numa-perfman/internal/namespace"
	"numa-perfman/internal/results"
)

// LockMetadata describes the active benchmark lock owner.
type LockMetadata struct {
	PID       int
	Host      string
	Command   string
	RunID     string
	Start     time.Time
	Namespace string
	Scenario  string
	ImageTag  string
	ImageRef  string
}

// RunLock is a held database-backed benchmark lock.
type RunLock struct {
	Key        string
	OwnerToken string
}

// RunLocker acquires exclusive benchmark locks backed by the results repository.
type RunLocker interface {
	Acquire(ctx context.Context, lockKey string, meta LockMetadata) (*RunLock, error)
	UpdateMetadata(ctx context.Context, l *RunLock, meta LockMetadata) error
	Release(ctx context.Context, l *RunLock) error
}

// BenchmarkLockKey returns the logical lock key for a benchmark namespace identity.
func BenchmarkLockKey(key namespace.BenchmarkKey) string {
	return "benchmark:" + key.Scenario + ":" + key.ImageTag
}

// ValidationLockKey returns the logical lock key for a validation namespace identity.
func ValidationLockKey(key namespace.ValidationKey) string {
	return "validation:" + key.Scenario + ":" + key.ImageTag
}

// DatabaseLocker acquires active locks through the results repository.
type DatabaseLocker struct {
	Repo *results.Repository
}

// Acquire inserts an active lock row when the key is free.
func (d DatabaseLocker) Acquire(ctx context.Context, lockKey string, meta LockMetadata) (*RunLock, error) {
	if d.Repo == nil {
		return nil, errors.New("results repository is required")
	}
	ownerToken := uuid.NewString()
	err := d.Repo.AcquireActiveLock(ctx, results.AcquireActiveLockParams{
		LockKey:    lockKey,
		OwnerToken: ownerToken,
		PID:        meta.PID,
		Host:       meta.Host,
		Command:    meta.Command,
		RunID:      meta.RunID,
		Namespace:  meta.Namespace,
		Scenario:   meta.Scenario,
		ImageRef:   meta.ImageRef,
		ImageTag:   meta.ImageTag,
		StartedAt:  meta.Start,
	})
	if err != nil {
		if errors.Is(err, results.ErrActiveLockHeld) {
			return nil, ErrLockHeld
		}
		return nil, err
	}
	return &RunLock{Key: lockKey, OwnerToken: ownerToken}, nil
}

// UpdateMetadata updates mutable lock fields for the owner token.
func (d DatabaseLocker) UpdateMetadata(ctx context.Context, l *RunLock, meta LockMetadata) error {
	if d.Repo == nil {
		return errors.New("results repository is required")
	}
	if l == nil || l.Key == "" || l.OwnerToken == "" {
		return results.ErrActiveLockNotHeld
	}
	pid := meta.PID
	host := meta.Host
	command := meta.Command
	runID := meta.RunID
	namespaceName := meta.Namespace
	scenario := meta.Scenario
	imageRef := meta.ImageRef
	imageTag := meta.ImageTag
	return d.Repo.UpdateActiveLock(ctx, l.Key, l.OwnerToken, results.UpdateActiveLockParams{
		PID:       &pid,
		Host:      &host,
		Command:   &command,
		RunID:     &runID,
		Namespace: &namespaceName,
		Scenario:  &scenario,
		ImageRef:  &imageRef,
		ImageTag:  &imageTag,
	})
}

// Release deletes the active lock row for the owner token.
func (d DatabaseLocker) Release(ctx context.Context, l *RunLock) error {
	if d.Repo == nil {
		return errors.New("results repository is required")
	}
	if l == nil || l.Key == "" || l.OwnerToken == "" {
		return results.ErrActiveLockNotHeld
	}
	return d.Repo.ReleaseActiveLock(ctx, l.Key, l.OwnerToken)
}
