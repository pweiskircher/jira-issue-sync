package orchestrator

import (
	"context"
	"fmt"

	"github.com/pat/jira-issue-sync/internal/contracts"
	"github.com/pat/jira-issue-sync/internal/output"
)

type Stage string

const (
	StagePush Stage = "push"
	StagePull Stage = "pull"
)

type Runner func(context.Context) (output.Report, error)

type Plan struct {
	Push Runner
	Pull Runner
}

func Execute(ctx context.Context, plan Plan) (output.Report, error) {
	if plan.Push == nil {
		return output.Report{}, fmt.Errorf("push stage is not configured")
	}
	if plan.Pull == nil {
		return output.Report{}, fmt.Errorf("pull stage is not configured")
	}

	report, err := runStage(ctx, StagePush, plan.Push)
	if err != nil {
		return report, err
	}

	pullReport, err := runStage(ctx, StagePull, plan.Pull)
	report = MergeReports(report, pullReport)
	if err != nil {
		return report, err
	}

	return report, nil
}

func runStage(ctx context.Context, stage Stage, runner Runner) (output.Report, error) {
	report, err := runner(ctx)
	if err != nil {
		return report, fmt.Errorf("failed to execute %s stage: %w", stage, err)
	}
	return report, nil
}

func MergeReports(left output.Report, right output.Report) output.Report {
	merged := output.Report{
		Counts: mergeCounts(left.Counts, right.Counts),
		Issues: append(append(make([]contracts.PerIssueResult, 0, len(left.Issues)+len(right.Issues)), left.Issues...), right.Issues...),
	}
	return merged
}

func mergeCounts(left, right contracts.AggregateCounts) contracts.AggregateCounts {
	return contracts.AggregateCounts{
		Processed: left.Processed + right.Processed,
		Updated:   left.Updated + right.Updated,
		Created:   left.Created + right.Created,
		Conflicts: left.Conflicts + right.Conflicts,
		Warnings:  left.Warnings + right.Warnings,
		Errors:    left.Errors + right.Errors,
	}
}
