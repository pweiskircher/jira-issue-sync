package commands

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/pat/jira-issue-sync/internal/contracts"
	"github.com/pat/jira-issue-sync/internal/issue"
	"github.com/pat/jira-issue-sync/internal/output"
)

type ViewOptions struct {
	Key string
}

func RunView(workDir string, options ViewOptions) (output.Report, error) {
	report := output.Report{CommandName: string(contracts.CommandView)}

	relativePath, err := findIssuePathByKey(workDir, options.Key)
	if err != nil {
		return report, err
	}

	content, err := os.ReadFile(filepath.Join(workDir, contracts.DefaultIssuesRootDir, relativePath))
	if err != nil {
		return report, err
	}

	doc, err := issue.ParseDocument(relativePath, string(content))
	if err != nil {
		addIssueResult(&report, contracts.PerIssueResult{
			Key:    strings.TrimSpace(options.Key),
			Action: "parse-error",
			Status: contracts.PerIssueStatusError,
			Messages: []contracts.IssueMessage{{
				Level:      "error",
				ReasonCode: contracts.ReasonCodeValidationFailed,
				Text:       err.Error(),
			}},
		})
		return report, nil
	}

	canonical, err := issue.RenderDocument(doc)
	if err != nil {
		return report, fmt.Errorf("failed to render document: %w", err)
	}

	addIssueResult(&report, contracts.PerIssueResult{
		Key:    doc.CanonicalKey,
		Action: "view",
		Status: contracts.PerIssueStatusSuccess,
		Messages: []contracts.IssueMessage{
			{Level: "info", Text: "path=" + relativePath},
			{Level: "info", Text: canonical},
		},
	})

	return report, nil
}
