package commands

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/pat/jira-issue-sync/internal/contracts"
	"github.com/pat/jira-issue-sync/internal/issue"
	"github.com/pat/jira-issue-sync/internal/output"
)

type StatusOptions struct {
	State            string
	Key              string
	IncludeUnchanged bool
}

func RunStatus(workDir string, options StatusOptions) (output.Report, error) {
	report := output.Report{CommandName: string(contracts.CommandStatus)}

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

		result := compareRecordAgainstSnapshot(workDir, record)
		if !options.IncludeUnchanged && result.Action == "unchanged" {
			continue
		}
		addIssueResult(&report, result)
	}

	return report, nil
}

func compareRecordAgainstSnapshot(workDir string, record issueRecord) contracts.PerIssueResult {
	snapshotRelativePath := filepath.Join(".sync", "originals", record.Key+".md")
	snapshotAbsolutePath := filepath.Join(workDir, contracts.DefaultIssuesRootDir, snapshotRelativePath)
	snapshotContent, err := os.ReadFile(snapshotAbsolutePath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			if contracts.LocalDraftKeyPattern.MatchString(record.Key) {
				return contracts.PerIssueResult{
					Key:    record.Key,
					Action: "new",
					Status: contracts.PerIssueStatusSuccess,
					Messages: []contracts.IssueMessage{{
						Level: "info",
						Text:  "no original snapshot found for local draft",
					}},
				}
			}
			return contracts.PerIssueResult{
				Key:    record.Key,
				Action: "local-conflict",
				Status: contracts.PerIssueStatusConflict,
				Messages: []contracts.IssueMessage{
					buildTypedDiagnostic(
						"error",
						contracts.ReasonCodeConflictBaseSnapshotMissing,
						"snapshot_missing",
						"original snapshot is missing",
						snapshotRelativePath,
					),
				},
			}
		}

		return contracts.PerIssueResult{
			Key:    record.Key,
			Action: "snapshot-error",
			Status: contracts.PerIssueStatusError,
			Messages: []contracts.IssueMessage{
				buildTypedDiagnostic("error", contracts.ReasonCodeValidationFailed, "snapshot_read_failed", err.Error(), snapshotRelativePath),
			},
		}
	}

	snapshotDoc, parseErr := issue.ParseDocument(snapshotRelativePath, string(snapshotContent))
	if parseErr != nil {
		reason := contracts.ReasonCodeValidationFailed
		code := "snapshot_parse_failed"
		if typed := asParseError(parseErr); typed != nil {
			reason = typed.ReasonCode
			code = string(typed.Code)
		}

		return contracts.PerIssueResult{
			Key:    record.Key,
			Action: "local-conflict",
			Status: contracts.PerIssueStatusConflict,
			Messages: []contracts.IssueMessage{
				buildTypedDiagnostic("error", reason, code, parseErr.Error(), snapshotRelativePath),
			},
		}
	}

	snapshotCanonical, renderErr := issue.RenderDocument(snapshotDoc)
	if renderErr != nil {
		return contracts.PerIssueResult{
			Key:    record.Key,
			Action: "local-conflict",
			Status: contracts.PerIssueStatusConflict,
			Messages: []contracts.IssueMessage{
				buildTypedDiagnostic("error", contracts.ReasonCodeValidationFailed, "snapshot_render_failed", renderErr.Error(), snapshotRelativePath),
			},
		}
	}

	if snapshotCanonical == record.Canonical {
		return contracts.PerIssueResult{
			Key:    record.Key,
			Action: "unchanged",
			Status: contracts.PerIssueStatusSuccess,
			Messages: []contracts.IssueMessage{{
				Level: "info",
				Text:  "local document matches original snapshot",
			}},
		}
	}

	return contracts.PerIssueResult{
		Key:    record.Key,
		Action: "modified",
		Status: contracts.PerIssueStatusSuccess,
		Messages: []contracts.IssueMessage{{
			Level: "info",
			Text:  "local document differs from original snapshot",
		}},
	}
}
