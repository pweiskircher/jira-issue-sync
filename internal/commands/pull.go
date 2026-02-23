package commands

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/pweiskircher/jira-issue-sync/internal/config"
	"github.com/pweiskircher/jira-issue-sync/internal/contracts"
	"github.com/pweiskircher/jira-issue-sync/internal/jira"
	"github.com/pweiskircher/jira-issue-sync/internal/output"
	"github.com/pweiskircher/jira-issue-sync/internal/store"
	pullsync "github.com/pweiskircher/jira-issue-sync/internal/sync/pull"
)

type PullOptions struct {
	Profile     string
	JQL         string
	PageSize    int
	Concurrency int
	Now         func() time.Time
	Environment config.Environment
	Adapter     jira.Adapter
}

func RunPull(ctx context.Context, workDir string, options PullOptions) (output.Report, error) {
	report := output.Report{CommandName: string(contracts.CommandPull)}

	cfg, err := config.Read(filepath.Join(workDir, contracts.DefaultConfigFilePath))
	if err != nil {
		return report, fmt.Errorf("failed to load config: %w", err)
	}

	environment := options.Environment
	if environment == (config.Environment{}) {
		environment = config.EnvironmentFromOS()
	}

	settings, err := config.Resolve(cfg, config.RuntimeFlags{Profile: options.Profile, JQL: options.JQL}, environment, config.ResolveOptions{RequireToken: true})
	if err != nil {
		return report, err
	}

	jql := strings.TrimSpace(settings.DefaultJQL)
	if jql == "" {
		return report, fmt.Errorf("failed to resolve runtime settings: no jql provided via --jql or config defaults")
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

	issueStore, err := store.New(filepath.Join(workDir, contracts.DefaultIssuesRootDir))
	if err != nil {
		return report, fmt.Errorf("failed to initialize issue store: %w", err)
	}

	now := options.Now
	if now == nil {
		now = time.Now
	}

	pipeline := pullsync.Pipeline{
		Adapter:            adapter,
		Store:              issueStore,
		Converter:          pullsync.NewADFMarkdownConverter(),
		PageSize:           options.PageSize,
		Concurrency:        options.Concurrency,
		Now:                now,
		CustomFieldAliases: settings.Profile.FieldConfig.Aliases,
		PullFields:         resolvePullFields(settings.Profile.FieldConfig),
	}

	result, err := pipeline.Execute(ctx, jql)
	if err != nil {
		if typed := asJiraError(err); typed != nil {
			return report, fmt.Errorf("failed to pull issues: %s", typed.Error())
		}
		return report, fmt.Errorf("failed to pull issues: %w", err)
	}

	for _, outcome := range result.Outcomes {
		report.Counts.Processed++
		if outcome.Updated {
			report.Counts.Updated++
		}
		if outcome.Status == contracts.PerIssueStatusError {
			report.Counts.Errors++
		}
		report.Issues = append(report.Issues, contracts.PerIssueResult{
			Key:      outcome.Key,
			Action:   outcome.Action,
			Status:   outcome.Status,
			Messages: outcome.Messages,
		})
	}

	return report, nil
}

func asJiraError(err error) *jira.Error {
	var typed *jira.Error
	if errors.As(err, &typed) {
		return typed
	}
	return nil
}

func resolvePullFields(fieldConfig contracts.FieldConfig) []string {
	mode := strings.ToLower(strings.TrimSpace(fieldConfig.FetchMode))
	fields := make([]string, 0)
	switch mode {
	case "all":
		fields = append(fields, "*all")
	case "explicit":
		// Only include explicit fields below.
	default:
		fields = append(fields, "*navigable")
	}

	seen := make(map[string]struct{})
	result := make([]string, 0, len(fields)+len(fieldConfig.IncludeFields))
	for _, field := range fields {
		trimmed := strings.TrimSpace(field)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		result = append(result, trimmed)
	}
	for _, field := range fieldConfig.IncludeFields {
		trimmed := strings.TrimSpace(field)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		result = append(result, trimmed)
	}

	excluded := make(map[string]struct{}, len(fieldConfig.ExcludeFields))
	for _, field := range fieldConfig.ExcludeFields {
		trimmed := strings.TrimSpace(field)
		if trimmed == "" {
			continue
		}
		excluded[trimmed] = struct{}{}
	}

	filtered := make([]string, 0, len(result))
	for _, field := range result {
		if _, excludedField := excluded[field]; excludedField {
			continue
		}
		filtered = append(filtered, field)
	}
	if len(filtered) == 0 {
		return []string{"*navigable"}
	}
	return filtered
}
