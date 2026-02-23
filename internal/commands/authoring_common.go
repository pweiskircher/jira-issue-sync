package commands

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/pweiskircher/jira-issue-sync/internal/contracts"
	"github.com/pweiskircher/jira-issue-sync/internal/issue"
)

func findIssuePathByKey(workDir string, key string) (string, error) {
	trimmedKey := strings.TrimSpace(key)
	if trimmedKey == "" {
		return "", fmt.Errorf("issue key is required")
	}

	if !contracts.JiraIssueKeyPattern.MatchString(trimmedKey) && !contracts.LocalDraftKeyPattern.MatchString(trimmedKey) {
		return "", fmt.Errorf("invalid issue key %q", key)
	}

	issuesRoot := filepath.Join(workDir, contracts.DefaultIssuesRootDir)
	stateDirs := []string{"open", "closed"}
	matches := make([]string, 0, 1)

	for _, stateDir := range stateDirs {
		dirPath := filepath.Join(issuesRoot, stateDir)
		entries, err := os.ReadDir(dirPath)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return "", err
		}

		for _, entry := range entries {
			if entry.IsDir() || strings.ToLower(filepath.Ext(entry.Name())) != ".md" {
				continue
			}

			relativePath := filepath.Join(stateDir, entry.Name())
			if filenameKey, ok := issue.ParseFilenameKey(relativePath); ok && filenameKey == trimmedKey {
				matches = append(matches, relativePath)
			}
		}
	}

	if len(matches) == 0 {
		return "", fmt.Errorf("issue %q not found in local workspace", trimmedKey)
	}
	if len(matches) > 1 {
		return "", fmt.Errorf("issue %q has ambiguous local matches", trimmedKey)
	}

	return matches[0], nil
}
