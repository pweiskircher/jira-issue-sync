package commands

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/pat/jira-issue-sync/internal/config"
	"github.com/pat/jira-issue-sync/internal/contracts"
	"github.com/pat/jira-issue-sync/internal/output"
	"github.com/pat/jira-issue-sync/internal/store"
)

type InitOptions struct {
	ProjectKey  string
	Profile     string
	JiraBaseURL string
	JiraEmail   string
	DefaultJQL  string
	ProfileJQL  string
	Force       bool
	IssuesRoot  string
	ConfigPath  string
}

func RunInit(workDir string, options InitOptions) (output.Report, error) {
	report := output.Report{CommandName: string(contracts.CommandInit)}

	projectKey := strings.TrimSpace(options.ProjectKey)
	if projectKey == "" {
		return report, fmt.Errorf("--project-key is required")
	}

	profile := strings.TrimSpace(options.Profile)
	if profile == "" {
		profile = "default"
	}

	issuesRoot := strings.TrimSpace(options.IssuesRoot)
	if issuesRoot == "" {
		issuesRoot = filepath.Join(workDir, contracts.DefaultIssuesRootDir)
	}

	configPath := strings.TrimSpace(options.ConfigPath)
	if configPath == "" {
		configPath = filepath.Join(workDir, contracts.DefaultConfigFilePath)
	}

	if !options.Force {
		if _, err := os.Stat(configPath); err == nil {
			return report, fmt.Errorf("config already exists at %s (use --force to overwrite)", configPath)
		}
	}

	workspaceStore, err := store.New(issuesRoot)
	if err != nil {
		return report, err
	}
	if err := workspaceStore.EnsureLayout(); err != nil {
		return report, err
	}

	cfg := contracts.Config{
		ConfigVersion: contracts.ConfigSchemaVersionV1,
		Jira: contracts.JiraConfig{
			BaseURL: strings.TrimSpace(options.JiraBaseURL),
			Email:   strings.TrimSpace(options.JiraEmail),
		},
		DefaultProfile: profile,
		DefaultJQL:     strings.TrimSpace(options.DefaultJQL),
		Profiles: map[string]contracts.ProjectProfile{
			profile: {
				ProjectKey: strings.ToUpper(projectKey),
				DefaultJQL: strings.TrimSpace(options.ProfileJQL),
			},
		},
	}

	if err := config.Write(configPath, cfg); err != nil {
		return report, err
	}

	action := "new"
	if options.Force {
		action = "modified"
	}
	addIssueResult(&report, contracts.PerIssueResult{
		Key:    "workspace",
		Action: action,
		Status: contracts.PerIssueStatusSuccess,
		Messages: []contracts.IssueMessage{{
			Level: "info",
			Text:  "config=" + configPath + " issues_root=" + issuesRoot + " profile=" + profile,
		}},
	})

	return report, nil
}
