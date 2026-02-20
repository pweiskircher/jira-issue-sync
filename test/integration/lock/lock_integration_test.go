package lockintegration

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/pat/jira-issue-sync/internal/lock"
)

func TestMutatingLockLifecycleAndStaleRecovery(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	lockPath := filepath.Join(workspace, ".issues", ".sync", "lock")
	locker := lock.NewFileLock(lockPath, lock.Options{
		StaleAfter:     1 * time.Second,
		AcquireTimeout: 500 * time.Millisecond,
		PollInterval:   10 * time.Millisecond,
	})

	lease, err := locker.Acquire(context.Background())
	if err != nil {
		t.Fatalf("acquire failed: %v", err)
	}
	if err := lease.Release(); err != nil {
		t.Fatalf("release failed: %v", err)
	}
	if _, err := os.Stat(lockPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected lock file cleanup, got: %v", err)
	}

	if err := os.MkdirAll(filepath.Dir(lockPath), 0o755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}
	if err := os.WriteFile(lockPath, []byte("stale\n"), 0o600); err != nil {
		t.Fatalf("write stale lock failed: %v", err)
	}
	staleTime := time.Now().Add(-5 * time.Minute)
	if err := os.Chtimes(lockPath, staleTime, staleTime); err != nil {
		t.Fatalf("chtimes failed: %v", err)
	}

	recovered, err := locker.Acquire(context.Background())
	if err != nil {
		t.Fatalf("stale recovery acquire failed: %v", err)
	}
	if !recovered.RecoveredStale() {
		t.Fatalf("expected stale lock recovery signal")
	}
	if err := recovered.Release(); err != nil {
		t.Fatalf("release after stale recovery failed: %v", err)
	}
}
