package orchestrator

import (
	"context"
	"errors"
	"testing"

	"github.com/pweiskircher/jira-issue-sync/internal/contracts"
	"github.com/pweiskircher/jira-issue-sync/internal/output"
)

func TestExecuteRunsPushThenPullAndMergesReports(t *testing.T) {
	t.Parallel()

	order := make([]Stage, 0, 2)
	report, err := Execute(context.Background(), Plan{
		Push: func(context.Context) (output.Report, error) {
			order = append(order, StagePush)
			return output.Report{
				Counts: contracts.AggregateCounts{Processed: 1, Updated: 1},
				Issues: []contracts.PerIssueResult{{Key: "PROJ-1", Action: "updated", Status: contracts.PerIssueStatusSuccess}},
			}, nil
		},
		Pull: func(context.Context) (output.Report, error) {
			order = append(order, StagePull)
			return output.Report{
				Counts: contracts.AggregateCounts{Processed: 2, Updated: 2, Errors: 1},
				Issues: []contracts.PerIssueResult{{Key: "PROJ-2", Action: "pull-error", Status: contracts.PerIssueStatusError}},
			}, nil
		},
	})
	if err != nil {
		t.Fatalf("execute failed: %v", err)
	}

	if len(order) != 2 || order[0] != StagePush || order[1] != StagePull {
		t.Fatalf("expected push-then-pull order, got %#v", order)
	}
	if report.Counts.Processed != 3 || report.Counts.Updated != 3 || report.Counts.Errors != 1 {
		t.Fatalf("unexpected merged counts: %#v", report.Counts)
	}
	if len(report.Issues) != 2 || report.Issues[0].Key != "PROJ-1" || report.Issues[1].Key != "PROJ-2" {
		t.Fatalf("unexpected merged issues: %#v", report.Issues)
	}
}

func TestExecuteStopsOnPushFatalError(t *testing.T) {
	t.Parallel()

	pullCalled := false
	report, err := Execute(context.Background(), Plan{
		Push: func(context.Context) (output.Report, error) {
			return output.Report{Counts: contracts.AggregateCounts{Processed: 2, Errors: 1}}, errors.New("push transport failed")
		},
		Pull: func(context.Context) (output.Report, error) {
			pullCalled = true
			return output.Report{}, nil
		},
	})
	if err == nil {
		t.Fatalf("expected push stage error")
	}
	if pullCalled {
		t.Fatalf("pull stage must not run after push fatal error")
	}
	if report.Counts.Processed != 2 || report.Counts.Errors != 1 {
		t.Fatalf("unexpected push report passthrough: %#v", report.Counts)
	}
}

func TestExecuteReturnsMergedReportOnPullFatalError(t *testing.T) {
	t.Parallel()

	report, err := Execute(context.Background(), Plan{
		Push: func(context.Context) (output.Report, error) {
			return output.Report{
				Counts: contracts.AggregateCounts{Processed: 1, Updated: 1},
				Issues: []contracts.PerIssueResult{{Key: "PROJ-1", Action: "updated", Status: contracts.PerIssueStatusSuccess}},
			}, nil
		},
		Pull: func(context.Context) (output.Report, error) {
			return output.Report{
				Counts: contracts.AggregateCounts{Processed: 1, Errors: 1},
				Issues: []contracts.PerIssueResult{{Key: "PROJ-2", Action: "pull-error", Status: contracts.PerIssueStatusError}},
			}, errors.New("pull transport failed")
		},
	})
	if err == nil {
		t.Fatalf("expected pull stage error")
	}
	if report.Counts.Processed != 2 || report.Counts.Updated != 1 || report.Counts.Errors != 1 {
		t.Fatalf("unexpected merged counts on pull fatal error: %#v", report.Counts)
	}
	if len(report.Issues) != 2 {
		t.Fatalf("expected merged issue list, got %#v", report.Issues)
	}
}
