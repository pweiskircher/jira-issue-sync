package commands

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/pat/jira-issue-sync/internal/config"
	"github.com/pat/jira-issue-sync/internal/contracts"
	"github.com/pat/jira-issue-sync/internal/output"
)

func TestRunSyncAggregatesPushThenPullReports(t *testing.T) {
	now := time.Date(2026, time.February, 20, 12, 0, 0, 0, time.UTC)
	env := config.Environment{JiraAPIToken: "token"}
	sequence := make([]string, 0, 2)

	originalPush := runPushCommand
	originalPull := runPullCommand
	t.Cleanup(func() {
		runPushCommand = originalPush
		runPullCommand = originalPull
	})

	runPushCommand = func(_ context.Context, workDir string, options PushOptions) (output.Report, error) {
		sequence = append(sequence, "push")
		if workDir != "/tmp/workspace" {
			t.Fatalf("unexpected workdir: %q", workDir)
		}
		if !options.DryRun {
			t.Fatalf("expected dry-run to propagate")
		}
		if options.Profile != "staging" {
			t.Fatalf("unexpected profile: %q", options.Profile)
		}
		if options.Now == nil || !options.Now().Equal(now) {
			t.Fatalf("expected now function to propagate")
		}
		if options.Environment != env {
			t.Fatalf("expected environment to propagate")
		}
		return output.Report{
			CommandName: "push",
			DryRun:      true,
			Counts:      contracts.AggregateCounts{Processed: 2, Updated: 1, Warnings: 1},
			Issues:      []contracts.PerIssueResult{{Key: "PROJ-1", Action: "updated", Status: contracts.PerIssueStatusWarning}},
		}, nil
	}

	runPullCommand = func(_ context.Context, workDir string, options PullOptions) (output.Report, error) {
		sequence = append(sequence, "pull")
		if workDir != "/tmp/workspace" {
			t.Fatalf("unexpected workdir: %q", workDir)
		}
		if options.Profile != "staging" || options.JQL != "project = PROJ" {
			t.Fatalf("unexpected pull profile/jql: %#v", options)
		}
		if options.PageSize != 50 || options.Concurrency != 3 {
			t.Fatalf("unexpected pull pagination options: %#v", options)
		}
		if options.Now == nil || !options.Now().Equal(now) {
			t.Fatalf("expected now function to propagate")
		}
		if options.Environment != env {
			t.Fatalf("expected environment to propagate")
		}
		return output.Report{
			CommandName: "pull",
			Counts:      contracts.AggregateCounts{Processed: 1, Updated: 1},
			Issues:      []contracts.PerIssueResult{{Key: "PROJ-2", Action: "pull", Status: contracts.PerIssueStatusSuccess}},
		}, nil
	}

	report, err := RunSync(context.Background(), "/tmp/workspace", SyncOptions{
		Profile:     "staging",
		JQL:         "project = PROJ",
		PageSize:    50,
		Concurrency: 3,
		DryRun:      true,
		Now:         func() time.Time { return now },
		Environment: env,
	})
	if err != nil {
		t.Fatalf("run sync failed: %v", err)
	}
	if len(sequence) != 2 || sequence[0] != "push" || sequence[1] != "pull" {
		t.Fatalf("expected push-then-pull order, got %#v", sequence)
	}
	if report.CommandName != string(contracts.CommandSync) {
		t.Fatalf("unexpected command name: %q", report.CommandName)
	}
	if !report.DryRun {
		t.Fatalf("expected sync report to preserve dry-run flag")
	}
	if report.Counts.Processed != 3 || report.Counts.Updated != 2 || report.Counts.Warnings != 1 {
		t.Fatalf("unexpected aggregate counts: %#v", report.Counts)
	}
	if len(report.Issues) != 2 || report.Issues[0].Key != "PROJ-1" || report.Issues[1].Key != "PROJ-2" {
		t.Fatalf("unexpected issue aggregation order: %#v", report.Issues)
	}
}

func TestRunSyncStopsOnPushFatalError(t *testing.T) {
	originalPush := runPushCommand
	originalPull := runPullCommand
	t.Cleanup(func() {
		runPushCommand = originalPush
		runPullCommand = originalPull
	})

	runPushCommand = func(context.Context, string, PushOptions) (output.Report, error) {
		return output.Report{Counts: contracts.AggregateCounts{Processed: 1, Errors: 1}}, errors.New("push transport failed")
	}
	pullCalled := false
	runPullCommand = func(context.Context, string, PullOptions) (output.Report, error) {
		pullCalled = true
		return output.Report{}, nil
	}

	report, err := RunSync(context.Background(), "/tmp/workspace", SyncOptions{})
	if err == nil {
		t.Fatalf("expected fatal sync error")
	}
	if pullCalled {
		t.Fatalf("pull must not execute when push stage fails fatally")
	}
	if report.Counts.Processed != 1 || report.Counts.Errors != 1 {
		t.Fatalf("unexpected push passthrough counts: %#v", report.Counts)
	}
}

func TestRunSyncReturnsMergedReportOnPullFatalError(t *testing.T) {
	originalPush := runPushCommand
	originalPull := runPullCommand
	t.Cleanup(func() {
		runPushCommand = originalPush
		runPullCommand = originalPull
	})

	runPushCommand = func(context.Context, string, PushOptions) (output.Report, error) {
		return output.Report{Counts: contracts.AggregateCounts{Processed: 1, Updated: 1}}, nil
	}
	runPullCommand = func(context.Context, string, PullOptions) (output.Report, error) {
		return output.Report{Counts: contracts.AggregateCounts{Processed: 1, Errors: 1}}, errors.New("pull transport failed")
	}

	report, err := RunSync(context.Background(), "/tmp/workspace", SyncOptions{})
	if err == nil {
		t.Fatalf("expected fatal sync error")
	}
	if report.Counts.Processed != 2 || report.Counts.Updated != 1 || report.Counts.Errors != 1 {
		t.Fatalf("unexpected merged counts on pull fatal error: %#v", report.Counts)
	}
}
