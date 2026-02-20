package lock

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestFileLockAcquireAndRelease(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), ".issues", ".sync", "lock")
	locker := NewFileLock(path, Options{
		AcquireTimeout: 500 * time.Millisecond,
		PollInterval:   10 * time.Millisecond,
	})

	lease, err := locker.Acquire(context.Background())
	if err != nil {
		t.Fatalf("expected lock acquisition, got: %v", err)
	}

	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected lock file to exist, got: %v", err)
	}

	if err := lease.Release(); err != nil {
		t.Fatalf("expected lock release, got: %v", err)
	}

	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected lock file removal, got: %v", err)
	}
}

func TestFileLockRecoversStaleLock(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), ".issues", ".sync", "lock")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}
	if err := os.WriteFile(path, []byte("stale\n"), 0o600); err != nil {
		t.Fatalf("write stale lock failed: %v", err)
	}

	staleTime := time.Now().Add(-5 * time.Minute)
	if err := os.Chtimes(path, staleTime, staleTime); err != nil {
		t.Fatalf("chtimes failed: %v", err)
	}

	locker := NewFileLock(path, Options{
		StaleAfter:     1 * time.Second,
		AcquireTimeout: 500 * time.Millisecond,
		PollInterval:   10 * time.Millisecond,
	})

	lease, err := locker.Acquire(context.Background())
	if err != nil {
		t.Fatalf("expected stale lock recovery, got: %v", err)
	}
	if !lease.RecoveredStale() {
		t.Fatalf("expected stale lock recovery flag")
	}

	if err := lease.Release(); err != nil {
		t.Fatalf("release failed: %v", err)
	}
}

func TestFileLockTimesOutWhenAlreadyHeld(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), ".issues", ".sync", "lock")
	primary := NewFileLock(path, Options{
		AcquireTimeout: 500 * time.Millisecond,
		PollInterval:   10 * time.Millisecond,
	})

	lease, err := primary.Acquire(context.Background())
	if err != nil {
		t.Fatalf("primary acquire failed: %v", err)
	}
	defer lease.Release()

	secondary := NewFileLock(path, Options{
		AcquireTimeout: 80 * time.Millisecond,
		PollInterval:   10 * time.Millisecond,
	})

	_, err = secondary.Acquire(context.Background())
	if !errors.Is(err, ErrAcquireTimeout) {
		t.Fatalf("expected acquire timeout, got: %v", err)
	}
}
