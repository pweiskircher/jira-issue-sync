package commands

import (
	"fmt"

	"github.com/pweiskircher/jira-issue-sync/internal/contracts"
	"github.com/pweiskircher/jira-issue-sync/internal/output"
)

type ListOptions struct {
	State string
	Key   string
}

func RunList(workDir string, options ListOptions) (output.Report, error) {
	report := output.Report{CommandName: string(contracts.CommandList)}

	filter, err := normalizeFilter(options.State, options.Key)
	if err != nil {
		return report, err
	}

	records, err := loadIssueRecords(workDir, filter)
	if err != nil {
		return report, fmt.Errorf("failed to read local issues: %w", err)
	}

	for _, record := range records {
		if record.Err != nil {
			addIssueResult(&report, contracts.PerIssueResult{
				Key:    record.Key,
				Action: "parse-error",
				Status: contracts.PerIssueStatusError,
				Messages: []contracts.IssueMessage{
					buildTypedDiagnostic("error", record.ReasonCode, record.ErrorCode, record.Err.Error(), record.RelativePath),
				},
			})
			continue
		}

		addIssueResult(&report, contracts.PerIssueResult{
			Key:    record.Key,
			Action: "list",
			Status: contracts.PerIssueStatusSuccess,
			Messages: []contracts.IssueMessage{
				{
					Level: "info",
					Text:  "path=" + record.RelativePath + " state=" + record.State + " summary=" + record.Document.FrontMatter.Summary,
				},
			},
		})
	}

	return report, nil
}
