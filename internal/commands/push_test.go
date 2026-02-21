package commands

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/pat/jira-issue-sync/internal/config"
	"github.com/pat/jira-issue-sync/internal/contracts"
	"github.com/pat/jira-issue-sync/internal/issue"
	"github.com/pat/jira-issue-sync/internal/jira"
)

func TestRunPushDryRunDoesNotMutateRemoteOrLocalState(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	writePushConfig(t, workspace)

	local := mustRenderDoc(t, issue.Document{FrontMatter: issue.FrontMatter{SchemaVersion: contracts.IssueFileSchemaVersionV1, Key: "PROJ-1", Summary: "Local summary", IssueType: "Task", Status: "To Do"}, CanonicalKey: "PROJ-1", MarkdownBody: "body"})
	original := mustRenderDoc(t, issue.Document{FrontMatter: issue.FrontMatter{SchemaVersion: contracts.IssueFileSchemaVersionV1, Key: "PROJ-1", Summary: "Remote summary", IssueType: "Task", Status: "To Do"}, CanonicalKey: "PROJ-1", MarkdownBody: "body"})

	writeIssueFile(t, workspace, filepath.Join("open", "PROJ-1-local.md"), local)
	snapshotPath := filepath.Join(workspace, contracts.DefaultIssuesRootDir, ".sync", "originals", "PROJ-1.md")
	writeIssueFile(t, workspace, filepath.Join(".sync", "originals", "PROJ-1.md"), original)
	before, err := os.ReadFile(snapshotPath)
	if err != nil {
		t.Fatalf("read snapshot before push failed: %v", err)
	}

	adapter := &pushAdapterStub{issues: map[string]jira.Issue{"PROJ-1": testRemoteIssue("PROJ-1", "Remote summary", "To Do")}}

	report, runErr := RunPush(context.Background(), workspace, PushOptions{DryRun: true, Adapter: adapter, Environment: config.Environment{JiraAPIToken: "token"}})
	if runErr != nil {
		t.Fatalf("run push failed: %v", runErr)
	}

	if adapter.updateCalls != 0 || adapter.applyCalls != 0 {
		t.Fatalf("dry-run must avoid remote writes, updates=%d transitions=%d", adapter.updateCalls, adapter.applyCalls)
	}

	after, err := os.ReadFile(snapshotPath)
	if err != nil {
		t.Fatalf("read snapshot after push failed: %v", err)
	}
	if string(before) != string(after) {
		t.Fatalf("dry-run must not rewrite snapshots")
	}
	if report.Counts.Errors != 0 || report.Counts.Conflicts != 0 || report.Counts.Warnings != 0 {
		t.Fatalf("unexpected dry-run counts: %#v", report.Counts)
	}
}

func TestRunPushContinuesAfterPerIssueFailures(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	writePushConfig(t, workspace)

	writePushIssue(t, workspace, "PROJ-1", "Local one", "Remote one", "To Do", "To Do")
	writePushIssue(t, workspace, "PROJ-2", "Local two", "Remote two", "To Do", "To Do")

	adapter := &pushAdapterStub{
		issues: map[string]jira.Issue{
			"PROJ-1": testRemoteIssue("PROJ-1", "Remote one", "To Do"),
			"PROJ-2": testRemoteIssue("PROJ-2", "Remote two", "To Do"),
		},
		updateErrByKey: map[string]error{"PROJ-1": errors.New("boom")},
	}

	report, runErr := RunPush(context.Background(), workspace, PushOptions{Adapter: adapter, Environment: config.Environment{JiraAPIToken: "token"}})
	if runErr != nil {
		t.Fatalf("run push failed: %v", runErr)
	}

	if report.Counts.Processed != 2 || report.Counts.Errors != 1 || report.Counts.Updated != 1 {
		t.Fatalf("unexpected push counts: %#v", report.Counts)
	}
	if adapter.updateCalls != 2 {
		t.Fatalf("expected two update attempts, got %d", adapter.updateCalls)
	}
}

func TestRunPushSkipsAmbiguousTransitionAndStillAppliesSafeUpdates(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	writePushConfig(t, workspace)
	writePushIssue(t, workspace, "PROJ-9", "Local updated", "Remote old", "Done", "To Do")

	adapter := &pushAdapterStub{
		issues: map[string]jira.Issue{"PROJ-9": testRemoteIssue("PROJ-9", "Remote old", "To Do")},
		transitionByKey: map[string]jira.TransitionResolution{
			"PROJ-9": {
				Kind:       jira.TransitionResolutionAmbiguous,
				ReasonCode: contracts.ReasonCodeTransitionAmbiguous,
			},
		},
	}

	report, runErr := RunPush(context.Background(), workspace, PushOptions{Adapter: adapter, Environment: config.Environment{JiraAPIToken: "token"}})
	if runErr != nil {
		t.Fatalf("run push failed: %v", runErr)
	}
	if report.Counts.Updated != 1 || report.Counts.Warnings != 1 {
		t.Fatalf("unexpected counts: %#v", report.Counts)
	}
	if adapter.updateCalls != 1 {
		t.Fatalf("expected one update call, got %d", adapter.updateCalls)
	}
	if adapter.applyCalls != 0 {
		t.Fatalf("ambiguous transition should not be applied")
	}
	if len(report.Issues) != 1 || report.Issues[0].Status != contracts.PerIssueStatusWarning {
		t.Fatalf("expected warning issue result, got %#v", report.Issues)
	}
}

func writePushIssue(t *testing.T, workspace string, key string, localSummary string, originalSummary string, localStatus string, originalStatus string) {
	t.Helper()

	local := mustRenderDoc(t, issue.Document{FrontMatter: issue.FrontMatter{SchemaVersion: contracts.IssueFileSchemaVersionV1, Key: key, Summary: localSummary, IssueType: "Task", Status: localStatus}, CanonicalKey: key, MarkdownBody: "body"})
	original := mustRenderDoc(t, issue.Document{FrontMatter: issue.FrontMatter{SchemaVersion: contracts.IssueFileSchemaVersionV1, Key: key, Summary: originalSummary, IssueType: "Task", Status: originalStatus}, CanonicalKey: key, MarkdownBody: "body"})
	writeIssueFile(t, workspace, filepath.Join("open", key+"-local.md"), local)
	writeIssueFile(t, workspace, filepath.Join(".sync", "originals", key+".md"), original)
}

func writePushConfig(t *testing.T, workspace string) {
	t.Helper()

	cfg := contracts.Config{ConfigVersion: contracts.ConfigSchemaVersionV1, Profiles: map[string]contracts.ProjectProfile{"default": {ProjectKey: "PROJ", DefaultJQL: "project = PROJ"}}}
	if err := config.Write(filepath.Join(workspace, contracts.DefaultConfigFilePath), cfg); err != nil {
		t.Fatalf("write config failed: %v", err)
	}
}

func testRemoteIssue(key string, summary string, status string) jira.Issue {
	return jira.Issue{Key: key, Fields: jira.IssueFields{Summary: summary, Description: []byte(`{"version":1,"type":"doc","content":[]}`), Status: &jira.StatusRef{Name: status}, IssueType: &jira.NamedRef{Name: "Task"}}}
}

type pushAdapterStub struct {
	issues          map[string]jira.Issue
	updateErrByKey  map[string]error
	transitionByKey map[string]jira.TransitionResolution
	updateCalls     int
	applyCalls      int
}

func (s *pushAdapterStub) SearchIssues(context.Context, jira.SearchIssuesRequest) (jira.SearchIssuesResponse, error) {
	panic("unexpected call")
}
func (s *pushAdapterStub) GetIssue(_ context.Context, issueKey string, _ []string) (jira.Issue, error) {
	if issue, ok := s.issues[issueKey]; ok {
		return issue, nil
	}
	return jira.Issue{}, errors.New("missing issue")
}
func (s *pushAdapterStub) CreateIssue(context.Context, jira.CreateIssueRequest) (jira.CreatedIssue, error) {
	panic("unexpected call")
}
func (s *pushAdapterStub) UpdateIssue(_ context.Context, issueKey string, _ jira.UpdateIssueRequest) error {
	s.updateCalls++
	if err, ok := s.updateErrByKey[issueKey]; ok {
		return err
	}
	return nil
}
func (s *pushAdapterStub) ListTransitions(context.Context, string) ([]jira.Transition, error) {
	panic("unexpected call")
}
func (s *pushAdapterStub) ApplyTransition(context.Context, string, string) error {
	s.applyCalls++
	return nil
}
func (s *pushAdapterStub) ResolveTransition(_ context.Context, issueKey string, _ contracts.TransitionSelection) (jira.TransitionResolution, error) {
	if resolution, ok := s.transitionByKey[issueKey]; ok {
		return resolution, nil
	}
	return jira.TransitionResolution{Kind: jira.TransitionResolutionUnavailable, ReasonCode: contracts.ReasonCodeTransitionUnavailable}, nil
}
