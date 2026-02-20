package output

import (
	"fmt"
	"time"

	"github.com/pat/jira-issue-sync/internal/contracts"
)

// pattern: Functional Core

// Report is command-level output data that can be rendered in human or JSON mode.
type Report struct {
	CommandName string
	DryRun      bool
	Counts      contracts.AggregateCounts
	Issues      []contracts.PerIssueResult
}

func BuildEnvelope(report Report, duration time.Duration) (contracts.CommandEnvelope, error) {
	env := contracts.CommandEnvelope{
		EnvelopeVersion: contracts.JSONEnvelopeVersionV1,
		Command: contracts.CommandMeta{
			Name:       report.CommandName,
			DurationMS: duration.Milliseconds(),
			DryRun:     report.DryRun,
		},
		Counts: report.Counts,
		Issues: report.Issues,
	}

	if err := contracts.ValidateEnvelopeBasics(env); err != nil {
		return contracts.CommandEnvelope{}, fmt.Errorf("failed to build command envelope: %w", err)
	}

	return env, nil
}

func ResolveExitCode(report Report, fatalErr error) contracts.ExitCode {
	return contracts.ResolveExitCode(report.Counts, fatalErr != nil)
}
