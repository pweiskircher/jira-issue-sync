package commands

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/pweiskircher/jira-issue-sync/internal/contracts"
	"github.com/pweiskircher/jira-issue-sync/internal/issue"
	"github.com/pweiskircher/jira-issue-sync/internal/output"
)

const (
	stateFilterAll    = "all"
	stateFilterOpen   = "open"
	stateFilterClosed = "closed"
)

type issueRecord struct {
	Key          string
	RelativePath string
	State        string
	Document     issue.Document
	Canonical    string
	Err          error
	ReasonCode   contracts.ReasonCode
	ErrorCode    string
}

type inspectFilter struct {
	state string
	key   string
}

func normalizeFilter(state string, key string) (inspectFilter, error) {
	normalizedState := strings.ToLower(strings.TrimSpace(state))
	if normalizedState == "" {
		normalizedState = stateFilterAll
	}
	switch normalizedState {
	case stateFilterAll, stateFilterOpen, stateFilterClosed:
	default:
		return inspectFilter{}, fmt.Errorf("invalid --state %q (expected all|open|closed)", state)
	}

	trimmedKey := strings.TrimSpace(key)
	if key != "" && trimmedKey == "" {
		return inspectFilter{}, fmt.Errorf("--key must not be only whitespace")
	}

	return inspectFilter{
		state: normalizedState,
		key:   strings.ToLower(trimmedKey),
	}, nil
}

func loadIssueRecords(workDir string, filter inspectFilter) ([]issueRecord, error) {
	issuesRoot := filepath.Join(workDir, contracts.DefaultIssuesRootDir)
	dirs := []string{stateFilterOpen, stateFilterClosed}
	if filter.state == stateFilterOpen {
		dirs = []string{stateFilterOpen}
	} else if filter.state == stateFilterClosed {
		dirs = []string{stateFilterClosed}
	}

	records := make([]issueRecord, 0)
	for _, stateDir := range dirs {
		files, err := os.ReadDir(filepath.Join(issuesRoot, stateDir))
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return nil, err
		}

		for _, file := range files {
			if file.IsDir() || strings.ToLower(filepath.Ext(file.Name())) != ".md" {
				continue
			}

			relativePath := filepath.Join(stateDir, file.Name())
			content, err := os.ReadFile(filepath.Join(issuesRoot, relativePath))
			if err != nil {
				return nil, err
			}

			record := issueRecord{RelativePath: relativePath, State: stateDir}
			doc, parseErr := issue.ParseDocument(relativePath, string(content))
			if parseErr != nil {
				record.Key = keyFromPath(relativePath)
				record.Err = parseErr
				record.ReasonCode = contracts.ReasonCodeValidationFailed
				record.ErrorCode = "parse_failed"
				if typed := asParseError(parseErr); typed != nil {
					record.ReasonCode = typed.ReasonCode
					record.ErrorCode = string(typed.Code)
				}
			} else {
				record.Key = doc.CanonicalKey
				record.Document = doc
				canonical, renderErr := issue.RenderDocument(doc)
				if renderErr != nil {
					record.Err = renderErr
					record.ReasonCode = contracts.ReasonCodeValidationFailed
					record.ErrorCode = "render_failed"
				} else {
					record.Canonical = canonical
				}
			}

			if filter.key != "" && !strings.Contains(strings.ToLower(record.Key), filter.key) {
				continue
			}

			records = append(records, record)
		}
	}

	sort.Slice(records, func(i, j int) bool {
		if records[i].Key == records[j].Key {
			return records[i].RelativePath < records[j].RelativePath
		}
		return records[i].Key < records[j].Key
	})

	return records, nil
}

func keyFromPath(relativePath string) string {
	if key, ok := issue.ParseFilenameKey(relativePath); ok {
		return key
	}
	return relativePath
}

func asParseError(err error) *issue.ParseError {
	var parseErr *issue.ParseError
	if errors.As(err, &parseErr) {
		return parseErr
	}
	return nil
}

func addIssueResult(report *output.Report, result contracts.PerIssueResult) {
	report.Issues = append(report.Issues, result)
	report.Counts.Processed++

	switch result.Status {
	case contracts.PerIssueStatusWarning:
		report.Counts.Warnings++
	case contracts.PerIssueStatusConflict:
		report.Counts.Conflicts++
	case contracts.PerIssueStatusError:
		report.Counts.Errors++
	}

	switch result.Action {
	case "new":
		report.Counts.Created++
	case "modified", "different":
		report.Counts.Updated++
	}
}

func buildTypedDiagnostic(level string, reason contracts.ReasonCode, code string, message string, path string) contracts.IssueMessage {
	text := strings.TrimSpace(message)
	if code != "" {
		text = "code=" + code + " " + text
	}
	if path != "" {
		text = text + " [path=" + path + "]"
	}
	return contracts.IssueMessage{
		Level:      level,
		ReasonCode: reason,
		Text:       strings.TrimSpace(text),
	}
}
