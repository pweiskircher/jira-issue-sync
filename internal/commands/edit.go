package commands

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/pat/jira-issue-sync/internal/contracts"
	"github.com/pat/jira-issue-sync/internal/editor"
	"github.com/pat/jira-issue-sync/internal/output"
)

type EditOptions struct {
	Key       string
	Editor    string
	RunEditor func(ctx context.Context, editor string, absolutePath string) error
}

func RunEdit(ctx context.Context, workDir string, options EditOptions) (output.Report, error) {
	report := output.Report{CommandName: string(contracts.CommandEdit)}

	relativePath, err := findIssuePathByKey(workDir, options.Key)
	if err != nil {
		return report, err
	}

	absolutePath := filepath.Join(workDir, contracts.DefaultIssuesRootDir, relativePath)
	editor := resolveEditor(options.Editor)
	if editor == "" {
		return report, fmt.Errorf("no editor configured (set --editor, VISUAL, or EDITOR)")
	}

	runner := options.RunEditor
	if runner == nil {
		runner = runEditor
	}
	if err := runner(ctx, editor, absolutePath); err != nil {
		return report, err
	}

	addIssueResult(&report, contracts.PerIssueResult{
		Key:    strings.TrimSpace(options.Key),
		Action: "modified",
		Status: contracts.PerIssueStatusSuccess,
		Messages: []contracts.IssueMessage{{
			Level: "info",
			Text:  "edited " + relativePath,
		}},
	})

	return report, nil
}

func resolveEditor(editorFlag string) string {
	if trimmed := strings.TrimSpace(editorFlag); trimmed != "" {
		return trimmed
	}
	if visual := strings.TrimSpace(os.Getenv("VISUAL")); visual != "" {
		return visual
	}
	return strings.TrimSpace(os.Getenv("EDITOR"))
}

func runEditor(ctx context.Context, editorCommand string, absolutePath string) error {
	return editor.Launch(ctx, editorCommand, absolutePath)
}
