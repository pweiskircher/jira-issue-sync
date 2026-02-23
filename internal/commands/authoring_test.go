package commands

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pweiskircher/jira-issue-sync/internal/contracts"
	"github.com/pweiskircher/jira-issue-sync/internal/issue"
	"github.com/pweiskircher/jira-issue-sync/internal/store"
)

func TestRunInitCreatesWorkspaceLayoutAndConfig(t *testing.T) {
	workspace := t.TempDir()

	report, err := RunInit(workspace, InitOptions{
		ProjectKey:  "PROJ",
		Profile:     "core",
		JiraBaseURL: "https://example.atlassian.net",
		JiraEmail:   "dev@example.com",
	})
	if err != nil {
		t.Fatalf("run init failed: %v", err)
	}

	if report.Counts.Created != 1 || report.Counts.Processed != 1 {
		t.Fatalf("unexpected counts: %#v", report.Counts)
	}

	mustExist := []string{
		filepath.Join(workspace, ".issues", "open"),
		filepath.Join(workspace, ".issues", "closed"),
		filepath.Join(workspace, ".issues", ".sync", "originals"),
		filepath.Join(workspace, ".issues", ".sync", "config.json"),
	}
	for _, path := range mustExist {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("expected path %s: %v", path, err)
		}
	}
}

func TestRunNewAndViewEndToEnd(t *testing.T) {
	workspace := t.TempDir()

	if _, err := RunInit(workspace, InitOptions{ProjectKey: "PROJ"}); err != nil {
		t.Fatalf("init failed: %v", err)
	}

	newReport, err := RunNew(workspace, NewOptions{
		Summary:   "Authoring flow",
		IssueType: "Task",
		Status:    "Open",
		Labels:    []string{"P1", "bug"},
		Body:      "This is the body.",
	})
	if err != nil {
		t.Fatalf("run new failed: %v", err)
	}
	if len(newReport.Issues) != 1 {
		t.Fatalf("expected one issue result, got %d", len(newReport.Issues))
	}

	key := newReport.Issues[0].Key
	if !contracts.LocalDraftKeyPattern.MatchString(key) {
		t.Fatalf("expected local draft key, got %q", key)
	}

	viewReport, err := RunView(workspace, ViewOptions{Key: key})
	if err != nil {
		t.Fatalf("run view failed: %v", err)
	}
	if len(viewReport.Issues) != 1 {
		t.Fatalf("expected one view result, got %d", len(viewReport.Issues))
	}

	messages := viewReport.Issues[0].Messages
	if len(messages) < 2 {
		t.Fatalf("expected path and document messages, got %#v", messages)
	}
	if !strings.Contains(messages[1].Text, "schema_version:") {
		t.Fatalf("expected rendered document in output message, got %q", messages[1].Text)
	}
}

func TestRunEditUsesConfiguredRunner(t *testing.T) {
	workspace := t.TempDir()
	issuesRoot := filepath.Join(workspace, contracts.DefaultIssuesRootDir)
	workspaceStore, err := store.New(issuesRoot)
	if err != nil {
		t.Fatalf("new store failed: %v", err)
	}

	doc := issue.Document{
		CanonicalKey: "PROJ-9",
		FrontMatter: issue.FrontMatter{
			SchemaVersion: contracts.IssueFileSchemaVersionV1,
			Key:           "PROJ-9",
			Summary:       "Editable",
			IssueType:     "Task",
			Status:        "Open",
		},
	}
	rendered, err := issue.RenderDocument(doc)
	if err != nil {
		t.Fatalf("render failed: %v", err)
	}
	if _, err := workspaceStore.WriteIssue(store.IssueStateOpen, "PROJ-9", "Editable", rendered); err != nil {
		t.Fatalf("write issue failed: %v", err)
	}

	called := false
	report, err := RunEdit(context.Background(), workspace, EditOptions{
		Key:    "PROJ-9",
		Editor: "fake-editor",
		RunEditor: func(ctx context.Context, editor string, absolutePath string) error {
			called = true
			if editor != "fake-editor" {
				t.Fatalf("unexpected editor %q", editor)
			}
			if !strings.HasSuffix(absolutePath, filepath.Join(".issues", "open", "PROJ-9-editable.md")) {
				t.Fatalf("unexpected path %q", absolutePath)
			}
			return nil
		},
	})
	if err != nil {
		t.Fatalf("run edit failed: %v", err)
	}
	if !called {
		t.Fatalf("expected edit runner to be called")
	}
	if report.Counts.Updated != 1 {
		t.Fatalf("expected one updated count, got %#v", report.Counts)
	}
}
