package cli

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	"numa-perfman/internal/results"
)

func TestRemoveStaleActiveLocksKeepsLiveOwnerWithoutRunID(t *testing.T) {
	ctx := context.Background()
	repo, err := results.Open(ctx, filepath.Join(t.TempDir(), "perfman.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()

	token := uuid.NewString()
	if err := repo.AcquireActiveLock(ctx, results.AcquireActiveLockParams{
		LockKey: "validation:map:v1.8.0", OwnerToken: token, PID: 42,
	}); err != nil {
		t.Fatal(err)
	}
	if err := removeStaleActiveLocks(ctx, repo, func(string) (bool, error) {
		return false, nil
	}, func(pid int) bool {
		return pid == 42
	}); err != nil {
		t.Fatal(err)
	}
	if _, found, err := repo.ReadActiveLock(ctx, "validation:map:v1.8.0"); err != nil || !found {
		t.Fatalf("live lock removed: found=%v err=%v", found, err)
	}
}
