package middleware

import (
	"context"
	"errors"

	"github.com/pat/jira-issue-sync/internal/contracts"
	"github.com/pat/jira-issue-sync/internal/lock"
)

type Runner func(ctx context.Context) error

func WithCommandLock(command contracts.CommandName, locker lock.Locker, next Runner) Runner {
	if next == nil {
		return nil
	}
	if locker == nil || !contracts.RequiresLock(command) {
		return next
	}

	return func(ctx context.Context) (runErr error) {
		lease, err := locker.Acquire(ctx)
		if err != nil {
			return err
		}

		defer func() {
			if releaseErr := lease.Release(); releaseErr != nil {
				if runErr == nil {
					runErr = releaseErr
					return
				}
				runErr = errors.Join(runErr, releaseErr)
			}
		}()

		return next(ctx)
	}
}
