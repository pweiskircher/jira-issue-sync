package lock

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/pweiskircher/jira-issue-sync/internal/contracts"
)

var ErrAcquireTimeout = errors.New("timed out acquiring lock")

type Lease interface {
	Release() error
	RecoveredStale() bool
}

type Locker interface {
	Acquire(ctx context.Context) (Lease, error)
}

type Options struct {
	StaleAfter     time.Duration
	AcquireTimeout time.Duration
	PollInterval   time.Duration
	Now            func() time.Time
}

type FileLock struct {
	path           string
	staleAfter     time.Duration
	acquireTimeout time.Duration
	pollInterval   time.Duration
	now            func() time.Time
}

type fileLease struct {
	path           string
	recoveredStale bool
	once           sync.Once
}

type lockFilePayload struct {
	PID       int    `json:"pid"`
	CreatedAt string `json:"created_at"`
}

func NewFileLock(path string, options Options) *FileLock {
	staleAfter := options.StaleAfter
	if staleAfter <= 0 {
		staleAfter = contracts.DefaultLockStaleAfter
	}

	acquireTimeout := options.AcquireTimeout
	if acquireTimeout <= 0 {
		acquireTimeout = contracts.DefaultLockAcquireTimeout
	}

	pollInterval := options.PollInterval
	if pollInterval <= 0 {
		pollInterval = contracts.DefaultLockPollInterval
	}

	now := options.Now
	if now == nil {
		now = time.Now
	}

	return &FileLock{
		path:           path,
		staleAfter:     staleAfter,
		acquireTimeout: acquireTimeout,
		pollInterval:   pollInterval,
		now:            now,
	}
}

func (l *FileLock) Acquire(ctx context.Context) (Lease, error) {
	if l == nil {
		return nil, errors.New("lock is nil")
	}

	if err := os.MkdirAll(filepath.Dir(l.path), 0o755); err != nil {
		return nil, err
	}

	if ctx == nil {
		ctx = context.Background()
	}

	deadline := l.now().Add(l.acquireTimeout)
	recoveredStale := false

	for {
		if err := l.tryCreateLock(); err == nil {
			return &fileLease{path: l.path, recoveredStale: recoveredStale}, nil
		} else if !errors.Is(err, os.ErrExist) {
			return nil, err
		}

		stale, err := l.lockIsStale()
		if err == nil && stale {
			if removeErr := os.Remove(l.path); removeErr == nil || errors.Is(removeErr, os.ErrNotExist) {
				recoveredStale = true
				continue
			}
		}

		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if !l.now().Before(deadline) {
			return nil, fmt.Errorf("%w: %s", ErrAcquireTimeout, l.path)
		}

		timer := time.NewTimer(l.pollInterval)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
}

func (l *FileLock) tryCreateLock() error {
	file, err := os.OpenFile(l.path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	defer file.Close()

	payload := lockFilePayload{PID: os.Getpid(), CreatedAt: l.now().UTC().Format(time.RFC3339Nano)}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	encoded = append(encoded, '\n')

	_, err = file.Write(encoded)
	return err
}

func (l *FileLock) lockIsStale() (bool, error) {
	info, err := os.Stat(l.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, err
	}

	age := l.now().Sub(info.ModTime())
	return age > l.staleAfter, nil
}

func (lease *fileLease) RecoveredStale() bool {
	if lease == nil {
		return false
	}
	return lease.recoveredStale
}

func (lease *fileLease) Release() error {
	if lease == nil {
		return nil
	}

	var releaseErr error
	lease.once.Do(func() {
		err := os.Remove(lease.path)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			releaseErr = err
		}
	})
	return releaseErr
}
