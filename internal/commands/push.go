package commands

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/pweiskircher/jira-issue-sync/internal/config"
	"github.com/pweiskircher/jira-issue-sync/internal/contracts"
	"github.com/pweiskircher/jira-issue-sync/internal/converter"
	"github.com/pweiskircher/jira-issue-sync/internal/issue"
	"github.com/pweiskircher/jira-issue-sync/internal/jira"
	"github.com/pweiskircher/jira-issue-sync/internal/output"
	"github.com/pweiskircher/jira-issue-sync/internal/store"
	publishsync "github.com/pweiskircher/jira-issue-sync/internal/sync/publish"
	pullsync "github.com/pweiskircher/jira-issue-sync/internal/sync/pull"
	pushexecute "github.com/pweiskircher/jira-issue-sync/internal/sync/push/execute"
)

var pushRemoteFields = []string{"summary", "description", "labels", "assignee", "priority", "status", "issuetype", "reporter", "created", "updated"}

type PushOptions struct {
	Profile     string
	DryRun      bool
	Now         func() time.Time
	Environment config.Environment
	Adapter     jira.Adapter
}

func RunPush(ctx context.Context, workDir string, options PushOptions) (output.Report, error) {
	report := output.Report{CommandName: string(contracts.CommandPush), DryRun: options.DryRun}

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
		adapter, err = jira.NewCloudAdapter(jira.CloudAdapterOptions{BaseURL: settings.JiraBaseURL, Email: settings.JiraEmail, APIToken: settings.JiraAPIToken})
		if err != nil {
			return report, fmt.Errorf("failed to initialize jira adapter: %w", err)
		}
	}

	records, err := loadIssueRecords(workDir, inspectFilter{state: stateFilterAll})
	if err != nil {
		return report, fmt.Errorf("failed to read local issues: %w", err)
	}

	workspaceStore, err := store.New(filepath.Join(workDir, contracts.DefaultIssuesRootDir))
	if err != nil {
		return report, fmt.Errorf("failed to initialize issue store: %w", err)
	}

	now := options.Now
	if now == nil {
		now = time.Now
	}

	pushConverter := pullsync.NewADFMarkdownConverter()
	for _, record := range records {
		if record.Err != nil {
			appendIssue(&report, contracts.PerIssueResult{Key: record.Key, Action: "parse-error", Status: contracts.PerIssueStatusError, Messages: []contracts.IssueMessage{buildTypedDiagnostic("error", record.ReasonCode, record.ErrorCode, record.Err.Error(), record.RelativePath)}})
			continue
		}

		if contracts.LocalDraftKeyPattern.MatchString(record.Key) {
			if options.DryRun {
				appendIssue(&report, contracts.PerIssueResult{
					Key:    record.Key,
					Action: "skipped",
					Status: contracts.PerIssueStatusSkipped,
					Messages: []contracts.IssueMessage{{
						Level:      "info",
						ReasonCode: contracts.ReasonCodeDryRunNoWrite,
						Text:       "dry-run: skipped draft publish",
					}},
				})
				continue
			}

			publishResult, publishErr := publishsync.PublishDraft(ctx, publishsync.Options{
				Adapter:    adapter,
				Store:      workspaceStore,
				Converter:  pushConverter,
				ProjectKey: settings.Profile.ProjectKey,
			}, publishsync.Input{
				LocalKey:     record.Key,
				RelativePath: record.RelativePath,
				Document:     record.Document,
			})
			if publishErr != nil {
				appendIssue(&report, contracts.PerIssueResult{
					Key:    record.Key,
					Action: "push-error",
					Status: contracts.PerIssueStatusError,
					Messages: []contracts.IssueMessage{{
						Level:      "error",
						ReasonCode: reasonFromPushError(publishErr),
						Text:       "failed to publish local draft: " + strings.TrimSpace(publishErr.Error()),
					}},
				})
				continue
			}

			appendIssue(&report, contracts.PerIssueResult{
				Key:    publishResult.RemoteKey,
				Action: "created",
				Status: contracts.PerIssueStatusSuccess,
				Messages: []contracts.IssueMessage{{
					Level: "info",
					Text:  "published local draft " + record.Key + " as " + publishResult.RemoteKey,
				}},
			})
			continue
		}

		comparison := compareRecordAgainstSnapshot(workDir, record)
		if comparison.Action == "unchanged" {
			continue
		}
		if comparison.Status == contracts.PerIssueStatusConflict || comparison.Status == contracts.PerIssueStatusError {
			appendIssue(&report, comparison)
			continue
		}

		originalDoc, err := readOriginalSnapshot(workDir, record.Key)
		if err != nil {
			appendIssue(&report, contracts.PerIssueResult{
				Key:    record.Key,
				Action: "snapshot-error",
				Status: contracts.PerIssueStatusError,
				Messages: []contracts.IssueMessage{{
					Level:      "error",
					ReasonCode: contracts.ReasonCodeValidationFailed,
					Text:       "failed to read original snapshot: " + strings.TrimSpace(err.Error()),
				}},
			})
			continue
		}

		remoteIssue, err := adapter.GetIssue(ctx, record.Key, pushRemoteFields)
		if err != nil {
			appendIssue(&report, contracts.PerIssueResult{
				Key:    record.Key,
				Action: "push-error",
				Status: contracts.PerIssueStatusError,
				Messages: []contracts.IssueMessage{{
					Level:      "error",
					ReasonCode: reasonFromPushError(err),
					Text:       "failed to fetch remote issue: " + strings.TrimSpace(err.Error()),
				}},
			})
			continue
		}

		remoteDoc, err := mapRemoteIssueToDocument(remoteIssue, now().UTC(), pushConverter)
		if err != nil {
			appendIssue(&report, contracts.PerIssueResult{
				Key:    record.Key,
				Action: "push-error",
				Status: contracts.PerIssueStatusError,
				Messages: []contracts.IssueMessage{{
					Level:      "error",
					ReasonCode: reasonFromPushError(err),
					Text:       "failed to prepare remote issue state: " + strings.TrimSpace(err.Error()),
				}},
			})
			continue
		}

		outcome := pushexecute.ExecuteIssue(ctx, pushexecute.Options{
			Adapter:             adapter,
			Converter:           pushConverter,
			DryRun:              options.DryRun,
			TransitionSelection: settings.ResolveTransitionSelection(record.Document.FrontMatter.Status),
		}, pushexecute.Input{Key: record.Key, Local: record.Document, Original: originalDoc, Remote: remoteDoc})

		appendIssue(&report, outcome.Result)
		if !options.DryRun && outcome.FullyApplied {
			canonicalLocal, renderErr := issue.RenderDocument(record.Document)
			if renderErr != nil {
				appendIssue(&report, contracts.PerIssueResult{Key: record.Key, Action: "snapshot-error", Status: contracts.PerIssueStatusError, Messages: []contracts.IssueMessage{{Level: "error", ReasonCode: contracts.ReasonCodeValidationFailed, Text: "failed to render local snapshot: " + strings.TrimSpace(renderErr.Error())}}})
				continue
			}
			if _, writeErr := workspaceStore.WriteOriginalSnapshot(record.Key, canonicalLocal); writeErr != nil {
				appendIssue(&report, contracts.PerIssueResult{Key: record.Key, Action: "snapshot-error", Status: contracts.PerIssueStatusError, Messages: []contracts.IssueMessage{{Level: "error", ReasonCode: contracts.ReasonCodeValidationFailed, Text: "failed to update original snapshot: " + strings.TrimSpace(writeErr.Error())}}})
				continue
			}
		}
	}

	return report, nil
}

func appendIssue(report *output.Report, result contracts.PerIssueResult) {
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
	case "updated":
		report.Counts.Updated++
	case "created":
		report.Counts.Created++
	}
}

func readOriginalSnapshot(workDir string, key string) (issue.Document, error) {
	snapshotRelativePath := filepath.Join(".sync", "originals", key+".md")
	content, err := os.ReadFile(filepath.Join(workDir, contracts.DefaultIssuesRootDir, snapshotRelativePath))
	if err != nil {
		return issue.Document{}, err
	}
	doc, err := issue.ParseDocument(snapshotRelativePath, string(content))
	if err != nil {
		return issue.Document{}, err
	}
	return doc, nil
}

func mapRemoteIssueToDocument(remote jira.Issue, syncedAt time.Time, markdownConverter converter.Adapter) (issue.Document, error) {
	rawADF := strings.TrimSpace(string(remote.Fields.Description))
	markdown, err := markdownConverter.ToMarkdown(rawADF)
	if err != nil {
		return issue.Document{}, err
	}

	canonicalADF := ""
	if rawADF != "" {
		canonicalADF, err = converter.ValidateAndCanonicalizeRawADF(rawADF)
		if err != nil {
			return issue.Document{}, err
		}
	}

	return issue.Document{
		CanonicalKey: strings.TrimSpace(remote.Key),
		FrontMatter: issue.FrontMatter{
			SchemaVersion: contracts.IssueFileSchemaVersionV1,
			Key:           strings.TrimSpace(remote.Key),
			Summary:       strings.TrimSpace(remote.Fields.Summary),
			IssueType:     namedRefValue(remote.Fields.IssueType),
			Status:        statusValue(remote.Fields.Status),
			Priority:      namedRefValue(remote.Fields.Priority),
			Assignee:      accountRefValue(remote.Fields.Assignee),
			Labels:        append([]string(nil), remote.Fields.Labels...),
			Reporter:      accountRefValue(remote.Fields.Reporter),
			CreatedAt:     strings.TrimSpace(remote.Fields.CreatedAt),
			UpdatedAt:     strings.TrimSpace(remote.Fields.UpdatedAt),
			SyncedAt:      syncedAt.Format(time.RFC3339Nano),
		},
		MarkdownBody: markdown.Markdown,
		RawADFJSON:   canonicalADF,
	}, nil
}

func reasonFromPushError(err error) contracts.ReasonCode {
	if typed := asJiraError(err); typed != nil && typed.ReasonCode != "" {
		return typed.ReasonCode
	}
	if typed := asConverterError(err); typed != nil && typed.ReasonCode != "" {
		return typed.ReasonCode
	}
	if typed := asParseError(err); typed != nil && typed.ReasonCode != "" {
		return typed.ReasonCode
	}
	return contracts.ReasonCodeValidationFailed
}

func asConverterError(err error) *converter.Error {
	var typed *converter.Error
	if errors.As(err, &typed) {
		return typed
	}
	return nil
}

func namedRefValue(ref *jira.NamedRef) string {
	if ref == nil {
		return ""
	}
	return strings.TrimSpace(ref.Name)
}

func statusValue(ref *jira.StatusRef) string {
	if ref == nil {
		return ""
	}
	return strings.TrimSpace(ref.Name)
}

func accountRefValue(ref *jira.AccountRef) string {
	if ref == nil {
		return ""
	}
	if value := strings.TrimSpace(ref.DisplayName); value != "" {
		return value
	}
	return strings.TrimSpace(ref.AccountID)
}
