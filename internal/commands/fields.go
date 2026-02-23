package commands

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/pweiskircher/jira-issue-sync/internal/config"
	"github.com/pweiskircher/jira-issue-sync/internal/contracts"
	"github.com/pweiskircher/jira-issue-sync/internal/jira"
	"github.com/pweiskircher/jira-issue-sync/internal/output"
)

type FieldsOptions struct {
	Profile     string
	All         bool
	Search      string
	Environment config.Environment
	Adapter     jira.Adapter
}

func RunFields(ctx context.Context, workDir string, options FieldsOptions) (output.Report, error) {
	report := output.Report{CommandName: "fields"}

	cfg, err := config.Read(filepath.Join(workDir, contracts.DefaultConfigFilePath))
	if err != nil {
		return report, fmt.Errorf("failed to load config: %w", err)
	}

	environment := options.Environment
	if environment == (config.Environment{}) {
		environment = config.EnvironmentFromOS()
	}

	settings, err := config.Resolve(cfg, config.RuntimeFlags{Profile: options.Profile}, environment, config.ResolveOptions{RequireToken: true})
	if err != nil {
		return report, err
	}

	adapter := options.Adapter
	if adapter == nil {
		adapter, err = jira.NewCloudAdapter(jira.CloudAdapterOptions{
			BaseURL:  settings.JiraBaseURL,
			Email:    settings.JiraEmail,
			APIToken: settings.JiraAPIToken,
		})
		if err != nil {
			return report, fmt.Errorf("failed to initialize jira adapter: %w", err)
		}
	}

	fields, err := adapter.ListFields(ctx)
	if err != nil {
		if typed := asJiraError(err); typed != nil {
			return report, fmt.Errorf("failed to list fields: %s", typed.Error())
		}
		return report, fmt.Errorf("failed to list fields: %w", err)
	}

	search := strings.ToLower(strings.TrimSpace(options.Search))
	filtered := make([]jira.FieldDefinition, 0, len(fields))
	for _, field := range fields {
		if !options.All && !field.Custom {
			continue
		}
		if search != "" {
			id := strings.ToLower(strings.TrimSpace(field.ID))
			name := strings.ToLower(strings.TrimSpace(field.Name))
			if !strings.Contains(id, search) && !strings.Contains(name, search) {
				continue
			}
		}
		filtered = append(filtered, field)
	}

	sort.Slice(filtered, func(i, j int) bool {
		return filtered[i].ID < filtered[j].ID
	})

	for _, field := range filtered {
		report.Counts.Processed++
		addIssueResult(&report, contracts.PerIssueResult{
			Key:    field.ID,
			Action: "field",
			Status: contracts.PerIssueStatusSuccess,
			Messages: []contracts.IssueMessage{{
				Level: "info",
				Text:  fmt.Sprintf("name=%s custom=%t", strings.TrimSpace(field.Name), field.Custom),
			}},
		})
	}

	return report, nil
}
