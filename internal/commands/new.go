package commands

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/pat/jira-issue-sync/internal/contracts"
	"github.com/pat/jira-issue-sync/internal/issue"
	"github.com/pat/jira-issue-sync/internal/output"
	"github.com/pat/jira-issue-sync/internal/store"
)

type NewOptions struct {
	Summary    string
	IssueType  string
	Status     string
	Priority   string
	Assignee   string
	Labels     []string
	Body       string
	IssuesRoot string
}

func RunNew(workDir string, options NewOptions) (output.Report, error) {
	report := output.Report{CommandName: string(contracts.CommandNew)}

	summary := strings.TrimSpace(options.Summary)
	if summary == "" {
		return report, fmt.Errorf("--summary is required")
	}

	issueType := strings.TrimSpace(options.IssueType)
	if issueType == "" {
		issueType = "Task"
	}

	status := strings.TrimSpace(options.Status)
	if status == "" {
		status = "Open"
	}

	issuesRoot := strings.TrimSpace(options.IssuesRoot)
	if issuesRoot == "" {
		issuesRoot = filepath.Join(workDir, contracts.DefaultIssuesRootDir)
	}

	workspaceStore, err := store.New(issuesRoot)
	if err != nil {
		return report, err
	}

	key, err := generateLocalDraftKey(issuesRoot)
	if err != nil {
		return report, err
	}

	doc := issue.Document{
		CanonicalKey: key,
		FrontMatter: issue.FrontMatter{
			SchemaVersion: contracts.IssueFileSchemaVersionV1,
			Key:           key,
			Summary:       summary,
			IssueType:     issueType,
			Status:        status,
			Priority:      strings.TrimSpace(options.Priority),
			Assignee:      strings.TrimSpace(options.Assignee),
			Labels:        append([]string(nil), options.Labels...),
		},
		MarkdownBody: strings.TrimSpace(options.Body),
	}

	canonical, err := issue.RenderDocument(doc)
	if err != nil {
		return report, err
	}

	relativePath, err := workspaceStore.WriteIssue(store.IssueStateOpen, key, summary, canonical)
	if err != nil {
		return report, err
	}

	addIssueResult(&report, contracts.PerIssueResult{
		Key:    key,
		Action: "new",
		Status: contracts.PerIssueStatusSuccess,
		Messages: []contracts.IssueMessage{{
			Level: "info",
			Text:  "created draft at " + relativePath,
		}},
	})

	return report, nil
}

func generateLocalDraftKey(issuesRoot string) (string, error) {
	for attempt := 0; attempt < 16; attempt++ {
		random := make([]byte, 3)
		if _, err := rand.Read(random); err != nil {
			return "", err
		}

		key := "L-" + hex.EncodeToString(random)
		candidatePrefix := key + "-"
		if !draftExists(issuesRoot, candidatePrefix) {
			return key, nil
		}
	}

	return "", fmt.Errorf("failed to generate unique local draft key")
}

func draftExists(issuesRoot string, filenamePrefix string) bool {
	dirs := []string{"open", "closed"}
	for _, dir := range dirs {
		entries, err := os.ReadDir(filepath.Join(issuesRoot, dir))
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			if strings.HasPrefix(entry.Name(), filenamePrefix) {
				return true
			}
		}
	}
	return false
}
