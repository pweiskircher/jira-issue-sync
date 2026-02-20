package middleware

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/pat/jira-issue-sync/internal/contracts"
	"github.com/pat/jira-issue-sync/internal/lock"
)

func TestWithCommandLockMutatingCommandAcquiresAndReleases(t *testing.T) {
	t.Parallel()

	lockPath := filepath.Join(t.TempDir(), ".issues", ".sync", "lock")
	locker := lock.NewFileLock(lockPath, lock.Options{
		AcquireTimeout: 500 * time.Millisecond,
		PollInterval:   10 * time.Millisecond,
	})

	runner := WithCommandLock(contracts.CommandInit, locker, func(ctx context.Context) error {
		if _, err := os.Stat(lockPath); err != nil {
			t.Fatalf("expected lock file while running, got: %v", err)
		}
		return nil
	})

	if err := runner(context.Background()); err != nil {
		t.Fatalf("runner failed: %v", err)
	}

	if _, err := os.Stat(lockPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected lock file to be removed, got: %v", err)
	}
}

func TestWithCommandLockReadOnlyCommandSkipsLock(t *testing.T) {
	t.Parallel()

	lockPath := filepath.Join(t.TempDir(), ".issues", ".sync", "lock")
	locker := lock.NewFileLock(lockPath, lock.Options{
		AcquireTimeout: 500 * time.Millisecond,
		PollInterval:   10 * time.Millisecond,
	})

	runner := WithCommandLock(contracts.CommandStatus, locker, func(ctx context.Context) error {
		if _, err := os.Stat(lockPath); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("expected no lock acquisition for read-only command, got: %v", err)
		}
		return nil
	})

	if err := runner(context.Background()); err != nil {
		t.Fatalf("runner failed: %v", err)
	}
}
