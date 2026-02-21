package commands

import (
	"context"
	"time"

	"github.com/pat/jira-issue-sync/internal/config"
	"github.com/pat/jira-issue-sync/internal/contracts"
	"github.com/pat/jira-issue-sync/internal/jira"
	"github.com/pat/jira-issue-sync/internal/output"
	"github.com/pat/jira-issue-sync/internal/sync/orchestrator"
)

type SyncOptions struct {
	Profile     string
	JQL         string
	PageSize    int
	Concurrency int
	DryRun      bool
	Now         func() time.Time
	Environment config.Environment
	Adapter     jira.Adapter
}

var runPushCommand = RunPush
var runPullCommand = RunPull

func RunSync(ctx context.Context, workDir string, options SyncOptions) (output.Report, error) {
	report := output.Report{CommandName: string(contracts.CommandSync), DryRun: options.DryRun}

	combined, err := orchestrator.Execute(ctx, orchestrator.Plan{
		Push: func(stageCtx context.Context) (output.Report, error) {
			return runPushCommand(stageCtx, workDir, PushOptions{
				Profile:     options.Profile,
				DryRun:      options.DryRun,
				Now:         options.Now,
				Environment: options.Environment,
				Adapter:     options.Adapter,
			})
		},
		Pull: func(stageCtx context.Context) (output.Report, error) {
			return runPullCommand(stageCtx, workDir, PullOptions{
				Profile:     options.Profile,
				JQL:         options.JQL,
				PageSize:    options.PageSize,
				Concurrency: options.Concurrency,
				Now:         options.Now,
				Environment: options.Environment,
				Adapter:     options.Adapter,
			})
		},
	})

	report.Counts = combined.Counts
	report.Issues = combined.Issues
	return report, err
}
