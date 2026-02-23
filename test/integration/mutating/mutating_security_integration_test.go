package mutatingintegration

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/pweiskircher/jira-issue-sync/internal/cli/middleware"
	"github.com/pweiskircher/jira-issue-sync/internal/commands"
	"github.com/pweiskircher/jira-issue-sync/internal/config"
	"github.com/pweiskircher/jira-issue-sync/internal/contracts"
	"github.com/pweiskircher/jira-issue-sync/internal/issue"
	"github.com/pweiskircher/jira-issue-sync/internal/jira"
	"github.com/pweiskircher/jira-issue-sync/internal/lock"
	"github.com/pweiskircher/jira-issue-sync/internal/output"
)

func TestMutatingCommandsEnforceLockAndRecoverStaleLock(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		command    contracts.CommandName
		prepareRun func(t *testing.T, workspace string) (func(context.Context) error, func(t *testing.T))
	}{
		{
			name:    "init",
			command: contracts.CommandInit,
			prepareRun: func(t *testing.T, workspace string) (func(context.Context) error, func(t *testing.T)) {
				run := func(context.Context) error {
					_, err := commands.RunInit(workspace, commands.InitOptions{ProjectKey: "PROJ", Profile: "default"})
					return err
				}
				verify := func(t *testing.T) {
					if _, err := os.Stat(filepath.Join(workspace, contracts.DefaultConfigFilePath)); err != nil {
						t.Fatalf("expected init to create config: %v", err)
					}
				}
				return run, verify
			},
		},
		{
			name:    "pull",
			command: contracts.CommandPull,
			prepareRun: func(t *testing.T, workspace string) (func(context.Context) error, func(t *testing.T)) {
				writeConfig(t, workspace, contracts.Config{
					ConfigVersion: contracts.ConfigSchemaVersionV1,
					Profiles: map[string]contracts.ProjectProfile{
						"default": {ProjectKey: "PROJ", DefaultJQL: "project = PROJ"},
					},
				})
				adapter := &integrationAdapterStub{pullIssues: []jira.Issue{remoteIssue("PROJ-101", "Pulled", "Open")}}
				run := func(ctx context.Context) error {
					_, err := commands.RunPull(ctx, workspace, commands.PullOptions{Adapter: adapter, Environment: config.Environment{JiraAPIToken: "token"}})
					return err
				}
				verify := func(t *testing.T) {
					if adapter.searchCalls == 0 {
						t.Fatalf("expected pull to query remote issues")
					}
				}
				return run, verify
			},
		},
		{
			name:    "push",
			command: contracts.CommandPush,
			prepareRun: func(t *testing.T, workspace string) (func(context.Context) error, func(t *testing.T)) {
				writeConfig(t, workspace, contracts.Config{
					ConfigVersion: contracts.ConfigSchemaVersionV1,
					Profiles: map[string]contracts.ProjectProfile{
						"default": {ProjectKey: "PROJ", DefaultJQL: "project = PROJ"},
					},
				})
				writeIssueDoc(t, workspace, filepath.Join("open", "PROJ-1-push.md"), testDocument("PROJ-1", "Local summary", "Done", "local"))
				writeIssueDoc(t, workspace, filepath.Join(".sync", "originals", "PROJ-1.md"), testDocument("PROJ-1", "Remote summary", "To Do", "local"))
				adapter := &integrationAdapterStub{issues: map[string]jira.Issue{"PROJ-1": remoteIssue("PROJ-1", "Remote summary", "To Do")}}
				run := func(ctx context.Context) error {
					_, err := commands.RunPush(ctx, workspace, commands.PushOptions{Adapter: adapter, Environment: config.Environment{JiraAPIToken: "token"}})
					return err
				}
				verify := func(t *testing.T) {
					if adapter.updateCalls == 0 {
						t.Fatalf("expected push to apply at least one remote update")
					}
				}
				return run, verify
			},
		},
		{
			name:    "sync",
			command: contracts.CommandSync,
			prepareRun: func(t *testing.T, workspace string) (func(context.Context) error, func(t *testing.T)) {
				writeConfig(t, workspace, contracts.Config{
					ConfigVersion: contracts.ConfigSchemaVersionV1,
					Profiles: map[string]contracts.ProjectProfile{
						"default": {ProjectKey: "PROJ", DefaultJQL: "project = PROJ"},
					},
				})
				writeIssueDoc(t, workspace, filepath.Join("open", "PROJ-2-sync.md"), testDocument("PROJ-2", "Local summary", "Done", "local"))
				writeIssueDoc(t, workspace, filepath.Join(".sync", "originals", "PROJ-2.md"), testDocument("PROJ-2", "Remote summary", "To Do", "local"))
				adapter := &integrationAdapterStub{issues: map[string]jira.Issue{"PROJ-2": remoteIssue("PROJ-2", "Remote summary", "To Do")}}
				run := func(ctx context.Context) error {
					_, err := commands.RunSync(ctx, workspace, commands.SyncOptions{Adapter: adapter, Environment: config.Environment{JiraAPIToken: "token"}})
					return err
				}
				verify := func(t *testing.T) {
					if adapter.updateCalls == 0 || adapter.searchCalls == 0 {
						t.Fatalf("expected sync to run push and pull paths; update=%d search=%d", adapter.updateCalls, adapter.searchCalls)
					}
				}
				return run, verify
			},
		},
		{
			name:    "draft publish",
			command: contracts.CommandPush,
			prepareRun: func(t *testing.T, workspace string) (func(context.Context) error, func(t *testing.T)) {
				writeConfig(t, workspace, contracts.Config{
					ConfigVersion: contracts.ConfigSchemaVersionV1,
					Profiles: map[string]contracts.ProjectProfile{
						"default": {ProjectKey: "PROJ", DefaultJQL: "project = PROJ"},
					},
				})
				localKey := "L-abcd12"
				writeIssueDoc(t, workspace, filepath.Join("open", localKey+"-draft.md"), testDocument(localKey, "Draft summary", "To Do", "See #L-abcd12"))
				adapter := &integrationAdapterStub{createdKeyBySummary: map[string]string{"Draft summary": "PROJ-500"}}
				run := func(ctx context.Context) error {
					_, err := commands.RunPush(ctx, workspace, commands.PushOptions{Adapter: adapter, Environment: config.Environment{JiraAPIToken: "token"}})
					return err
				}
				verify := func(t *testing.T) {
					if adapter.createCalls == 0 {
						t.Fatalf("expected draft publish to create remote issue")
					}
				}
				return run, verify
			},
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			workspace := t.TempDir()
			run, verify := tc.prepareRun(t, workspace)
			lockPath := filepath.Join(workspace, contracts.DefaultLockFilePath)

			if err := os.MkdirAll(filepath.Dir(lockPath), 0o755); err != nil {
				t.Fatalf("mkdir lock dir failed: %v", err)
			}
			if err := os.WriteFile(lockPath, []byte("fresh\n"), 0o600); err != nil {
				t.Fatalf("write fresh lock failed: %v", err)
			}

			executed := 0
			freshRunner := middleware.WithCommandLock(tc.command, lock.NewFileLock(lockPath, lock.Options{
				StaleAfter:     10 * time.Minute,
				AcquireTimeout: 80 * time.Millisecond,
				PollInterval:   10 * time.Millisecond,
			}), func(ctx context.Context) error {
				executed++
				return run(ctx)
			})

			err := freshRunner(context.Background())
			if !errors.Is(err, lock.ErrAcquireTimeout) {
				t.Fatalf("expected lock timeout for fresh lock, got %v", err)
			}
			if executed != 0 {
				t.Fatalf("command executed despite held lock")
			}

			if err := os.WriteFile(lockPath, []byte("stale\n"), 0o600); err != nil {
				t.Fatalf("rewrite stale lock failed: %v", err)
			}
			staleTime := time.Now().Add(-5 * time.Minute)
			if err := os.Chtimes(lockPath, staleTime, staleTime); err != nil {
				t.Fatalf("chtimes stale lock failed: %v", err)
			}

			executed = 0
			staleRunner := middleware.WithCommandLock(tc.command, lock.NewFileLock(lockPath, lock.Options{
				StaleAfter:     1 * time.Second,
				AcquireTimeout: 300 * time.Millisecond,
				PollInterval:   10 * time.Millisecond,
			}), func(ctx context.Context) error {
				executed++
				return run(ctx)
			})
			if err := staleRunner(context.Background()); err != nil {
				t.Fatalf("expected stale-lock recovery run success, got %v", err)
			}
			if executed != 1 {
				t.Fatalf("expected command to execute once after stale recovery, got %d", executed)
			}
			verify(t)
		})
	}
}

func TestPushDryRunHasNoRemoteOrLocalWritesForUpdateAndDraftPublish(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	writeConfig(t, workspace, contracts.Config{
		ConfigVersion: contracts.ConfigSchemaVersionV1,
		Profiles: map[string]contracts.ProjectProfile{
			"default": {ProjectKey: "PROJ", DefaultJQL: "project = PROJ"},
		},
	})

	writeIssueDoc(t, workspace, filepath.Join("open", "PROJ-7-update.md"), testDocument("PROJ-7", "Local changed", "To Do", "body"))
	writeIssueDoc(t, workspace, filepath.Join(".sync", "originals", "PROJ-7.md"), testDocument("PROJ-7", "Remote summary", "To Do", "body"))
	snapshotPath := filepath.Join(workspace, contracts.DefaultIssuesRootDir, ".sync", "originals", "PROJ-7.md")
	beforeSnapshot, err := os.ReadFile(snapshotPath)
	if err != nil {
		t.Fatalf("read snapshot before failed: %v", err)
	}

	draftPath := filepath.Join("open", "L-feed12-draft.md")
	writeIssueDoc(t, workspace, draftPath, testDocument("L-feed12", "Draft dry run", "To Do", "See #L-feed12"))

	adapter := &integrationAdapterStub{
		issues:              map[string]jira.Issue{"PROJ-7": remoteIssue("PROJ-7", "Remote summary", "To Do")},
		createdKeyBySummary: map[string]string{"Draft dry run": "PROJ-777"},
	}

	report, runErr := commands.RunPush(context.Background(), workspace, commands.PushOptions{
		DryRun:      true,
		Adapter:     adapter,
		Environment: config.Environment{JiraAPIToken: "token"},
	})
	if runErr != nil {
		t.Fatalf("run push dry-run failed: %v", runErr)
	}

	if adapter.updateCalls != 0 || adapter.applyCalls != 0 || adapter.createCalls != 0 {
		t.Fatalf("dry-run must not perform remote writes; update=%d apply=%d create=%d", adapter.updateCalls, adapter.applyCalls, adapter.createCalls)
	}

	afterSnapshot, err := os.ReadFile(snapshotPath)
	if err != nil {
		t.Fatalf("read snapshot after failed: %v", err)
	}
	if string(beforeSnapshot) != string(afterSnapshot) {
		t.Fatalf("dry-run must not rewrite original snapshot")
	}

	if _, err := os.Stat(filepath.Join(workspace, contracts.DefaultIssuesRootDir, draftPath)); err != nil {
		t.Fatalf("dry-run must keep draft file unchanged: %v", err)
	}
	if _, err := os.Stat(filepath.Join(workspace, contracts.DefaultOpenDir, "PROJ-777-draft-dry-run.md")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("dry-run must not create published draft file")
	}

	if !reportContainsReason(report, contracts.ReasonCodeDryRunNoWrite) {
		t.Fatalf("expected dry-run report to include typed no-write reason code, got %#v", report.Issues)
	}
}

func TestPushRecoversFromMalformedPreviouslyPulledFilesWithTypedDiagnostics(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	writeConfig(t, workspace, contracts.Config{
		ConfigVersion: contracts.ConfigSchemaVersionV1,
		Profiles: map[string]contracts.ProjectProfile{
			"default": {ProjectKey: "PROJ", DefaultJQL: "project = PROJ"},
		},
	})

	writeIssueDoc(t, workspace, filepath.Join("open", "PROJ-1-valid.md"), testDocument("PROJ-1", "Local changed", "To Do", "body"))
	writeIssueDoc(t, workspace, filepath.Join(".sync", "originals", "PROJ-1.md"), testDocument("PROJ-1", "Remote summary", "To Do", "body"))

	writeRawIssueFile(t, workspace, filepath.Join("open", "PROJ-2-bad-front-matter.md"), "not-front-matter")
	writeRawIssueFile(t, workspace, filepath.Join("open", "PROJ-3-bad-adf.md"), strings.Join([]string{
		"---",
		"schema_version: \"1\"",
		"key: \"PROJ-3\"",
		"summary: \"Bad adf\"",
		"issue_type: \"Task\"",
		"status: \"Open\"",
		"---",
		"",
		"body",
		"",
		"```jira-adf",
		"{\"version\":1,\"type\":\"doc\",\"content\":[}",
		"```",
	}, "\n"))

	adapter := &integrationAdapterStub{issues: map[string]jira.Issue{"PROJ-1": remoteIssue("PROJ-1", "Remote summary", "To Do")}}
	report, runErr := commands.RunPush(context.Background(), workspace, commands.PushOptions{Adapter: adapter, Environment: config.Environment{JiraAPIToken: "token"}})
	if runErr != nil {
		t.Fatalf("run push failed: %v", runErr)
	}

	if report.Counts.Processed != 3 || report.Counts.Errors != 2 || report.Counts.Updated != 1 {
		t.Fatalf("unexpected counts for mixed recoverable push run: %#v", report.Counts)
	}

	byKey := map[string]contracts.PerIssueResult{}
	for _, result := range report.Issues {
		byKey[result.Key] = result
	}
	if got := byKey["PROJ-2"]; got.Status != contracts.PerIssueStatusError || firstReason(got) != contracts.ReasonCodeValidationFailed {
		t.Fatalf("expected deterministic front-matter parse diagnostic, got %#v", got)
	}
	if got := byKey["PROJ-3"]; got.Status != contracts.PerIssueStatusError || firstReason(got) != contracts.ReasonCodeDescriptionADFBlockMalformed {
		t.Fatalf("expected deterministic raw-adf parse diagnostic, got %#v", got)
	}
	if got := byKey["PROJ-1"]; got.Status != contracts.PerIssueStatusSuccess || got.Action != "updated" {
		t.Fatalf("expected valid issue to keep processing despite malformed neighbors, got %#v", got)
	}
}

func firstReason(result contracts.PerIssueResult) contracts.ReasonCode {
	if len(result.Messages) == 0 {
		return ""
	}
	return result.Messages[0].ReasonCode
}

func reportContainsReason(report output.Report, code contracts.ReasonCode) bool {
	for _, item := range report.Issues {
		for _, message := range item.Messages {
			if message.ReasonCode == code {
				return true
			}
		}
	}
	return false
}

func writeConfig(t *testing.T, workspace string, cfg contracts.Config) {
	t.Helper()
	if err := config.Write(filepath.Join(workspace, contracts.DefaultConfigFilePath), cfg); err != nil {
		t.Fatalf("write config failed: %v", err)
	}
}

func testDocument(key string, summary string, status string, body string) issue.Document {
	return issue.Document{
		CanonicalKey: key,
		FrontMatter: issue.FrontMatter{
			SchemaVersion: contracts.IssueFileSchemaVersionV1,
			Key:           key,
			Summary:       summary,
			IssueType:     "Task",
			Status:        status,
		},
		MarkdownBody: body,
	}
}

func writeIssueDoc(t *testing.T, workspace string, relativePath string, doc issue.Document) {
	t.Helper()
	rendered, err := issue.RenderDocument(doc)
	if err != nil {
		t.Fatalf("render document failed: %v", err)
	}
	writeRawIssueFile(t, workspace, relativePath, rendered)
}

func writeRawIssueFile(t *testing.T, workspace string, relativePath string, content string) {
	t.Helper()
	path := filepath.Join(workspace, contracts.DefaultIssuesRootDir, relativePath)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write file failed: %v", err)
	}
}

func remoteIssue(key string, summary string, status string) jira.Issue {
	return jira.Issue{
		Key: key,
		Fields: jira.IssueFields{
			Summary:     summary,
			Description: json.RawMessage(`{"version":1,"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"body"}]}]}`),
			Status:      &jira.StatusRef{Name: status},
			IssueType:   &jira.NamedRef{Name: "Task"},
		},
	}
}

type integrationAdapterStub struct {
	pullIssues          []jira.Issue
	issues              map[string]jira.Issue
	transitionByKey     map[string]jira.TransitionResolution
	createdKeyBySummary map[string]string
	selectionByIssue    map[string]contracts.TransitionSelection
	searchCalls         int
	getCalls            int
	createCalls         int
	updateCalls         int
	applyCalls          int
}

func (s *integrationAdapterStub) SearchIssues(_ context.Context, request jira.SearchIssuesRequest) (jira.SearchIssuesResponse, error) {
	s.searchCalls++
	if request.StartAt > 0 || len(s.pullIssues) == 0 {
		return jira.SearchIssuesResponse{StartAt: request.StartAt, Total: len(s.pullIssues), Issues: nil}, nil
	}
	return jira.SearchIssuesResponse{StartAt: request.StartAt, Total: len(s.pullIssues), Issues: s.pullIssues}, nil
}

func (s *integrationAdapterStub) ListFields(context.Context) ([]jira.FieldDefinition, error) {
	return nil, nil
}

func (s *integrationAdapterStub) GetIssue(_ context.Context, issueKey string, _ []string) (jira.Issue, error) {
	s.getCalls++
	if issue, ok := s.issues[issueKey]; ok {
		return issue, nil
	}
	return jira.Issue{}, errors.New("missing issue")
}

func (s *integrationAdapterStub) CreateIssue(_ context.Context, request jira.CreateIssueRequest) (jira.CreatedIssue, error) {
	s.createCalls++
	if key, ok := s.createdKeyBySummary[request.Summary]; ok {
		return jira.CreatedIssue{Key: key}, nil
	}
	return jira.CreatedIssue{Key: "PROJ-999"}, nil
}

func (s *integrationAdapterStub) UpdateIssue(context.Context, string, jira.UpdateIssueRequest) error {
	s.updateCalls++
	return nil
}

func (s *integrationAdapterStub) ListTransitions(context.Context, string) ([]jira.Transition, error) {
	return nil, nil
}

func (s *integrationAdapterStub) ApplyTransition(context.Context, string, string) error {
	s.applyCalls++
	return nil
}

func (s *integrationAdapterStub) ResolveTransition(_ context.Context, issueKey string, selection contracts.TransitionSelection) (jira.TransitionResolution, error) {
	if s.selectionByIssue == nil {
		s.selectionByIssue = map[string]contracts.TransitionSelection{}
	}
	s.selectionByIssue[issueKey] = selection
	if resolution, ok := s.transitionByKey[issueKey]; ok {
		return resolution, nil
	}
	return jira.TransitionResolution{Kind: jira.TransitionResolutionUnavailable, ReasonCode: contracts.ReasonCodeTransitionUnavailable}, nil
}
