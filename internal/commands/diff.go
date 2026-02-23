package commands

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/pweiskircher/jira-issue-sync/internal/contracts"
	"github.com/pweiskircher/jira-issue-sync/internal/issue"
	"github.com/pweiskircher/jira-issue-sync/internal/output"
)

type DiffOptions struct {
	State            string
	Key              string
	IncludeUnchanged bool
}

func RunDiff(workDir string, options DiffOptions) (output.Report, error) {
	report := output.Report{CommandName: string(contracts.CommandDiff)}

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

		result := buildDiffResult(workDir, record)
		if !options.IncludeUnchanged && result.Action == "unchanged" {
			continue
		}
		addIssueResult(&report, result)
	}

	return report, nil
}

func buildDiffResult(workDir string, record issueRecord) contracts.PerIssueResult {
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
						Text:  deterministicDiff("", record.Canonical),
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
				Text:  "no local differences",
			}},
		}
	}

	return contracts.PerIssueResult{
		Key:    record.Key,
		Action: "different",
		Status: contracts.PerIssueStatusSuccess,
		Messages: []contracts.IssueMessage{{
			Level: "info",
			Text:  deterministicDiff(snapshotCanonical, record.Canonical),
		}},
	}
}

func deterministicDiff(original string, local string) string {
	originalLines := splitLines(original)
	localLines := splitLines(local)

	var builder strings.Builder
	builder.WriteString("--- original\n")
	builder.WriteString("+++ local\n")

	i := 0
	j := 0
	for i < len(originalLines) || j < len(localLines) {
		if i < len(originalLines) && j < len(localLines) && originalLines[i] == localLines[j] {
			i++
			j++
			continue
		}

		if i < len(originalLines) && j+1 < len(localLines) && originalLines[i] == localLines[j+1] {
			builder.WriteString("+ ")
			builder.WriteString(localLines[j])
			builder.WriteString("\n")
			j++
			continue
		}

		if i+1 < len(originalLines) && j < len(localLines) && originalLines[i+1] == localLines[j] {
			builder.WriteString("- ")
			builder.WriteString(originalLines[i])
			builder.WriteString("\n")
			i++
			continue
		}

		if i < len(originalLines) {
			builder.WriteString("- ")
			builder.WriteString(originalLines[i])
			builder.WriteString("\n")
			i++
		}
		if j < len(localLines) {
			builder.WriteString("+ ")
			builder.WriteString(localLines[j])
			builder.WriteString("\n")
			j++
		}
	}

	return strings.TrimRight(builder.String(), "\n")
}

func splitLines(input string) []string {
	normalized := contracts.NormalizeSingleValue(contracts.NormalizationNormalizeLineEndings, input)
	normalized = strings.TrimSuffix(normalized, "\n")
	if normalized == "" {
		return nil
	}
	return strings.Split(normalized, "\n")
}
