package commands

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pat/jira-issue-sync/internal/contracts"
	"github.com/pat/jira-issue-sync/internal/issue"
)

func TestRunStatusReportsChangesConflictsAndTypedDiagnostics(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()

	writeIssueFile(t, workspace, filepath.Join("open", "PROJ-1-unchanged.md"), mustRenderDoc(t, issue.Document{
		FrontMatter: issue.FrontMatter{
			SchemaVersion: contracts.IssueFileSchemaVersionV1,
			Key:           "PROJ-1",
			Summary:       "Unchanged",
			IssueType:     "Task",
			Status:        "Open",
		},
		CanonicalKey: "PROJ-1",
		MarkdownBody: "same",
	}))
	writeIssueFile(t, workspace, filepath.Join(".sync", "originals", "PROJ-1.md"), mustRenderDoc(t, issue.Document{
		FrontMatter: issue.FrontMatter{
			SchemaVersion: contracts.IssueFileSchemaVersionV1,
			Key:           "PROJ-1",
			Summary:       "Unchanged",
			IssueType:     "Task",
			Status:        "Open",
		},
		CanonicalKey: "PROJ-1",
		MarkdownBody: "same",
	}))

	writeIssueFile(t, workspace, filepath.Join("open", "PROJ-2-modified.md"), mustRenderDoc(t, issue.Document{
		FrontMatter: issue.FrontMatter{
			SchemaVersion: contracts.IssueFileSchemaVersionV1,
			Key:           "PROJ-2",
			Summary:       "Modified local summary",
			IssueType:     "Task",
			Status:        "Open",
		},
		CanonicalKey: "PROJ-2",
		MarkdownBody: "local-body",
	}))
	writeIssueFile(t, workspace, filepath.Join(".sync", "originals", "PROJ-2.md"), mustRenderDoc(t, issue.Document{
		FrontMatter: issue.FrontMatter{
			SchemaVersion: contracts.IssueFileSchemaVersionV1,
			Key:           "PROJ-2",
			Summary:       "Original summary",
			IssueType:     "Task",
			Status:        "Open",
		},
		CanonicalKey: "PROJ-2",
		MarkdownBody: "local-body",
	}))

	writeIssueFile(t, workspace, filepath.Join("open", "L-abcd1234-new.md"), mustRenderDoc(t, issue.Document{
		FrontMatter: issue.FrontMatter{
			SchemaVersion: contracts.IssueFileSchemaVersionV1,
			Key:           "L-abcd1234",
			Summary:       "New draft",
			IssueType:     "Task",
			Status:        "Open",
		},
		CanonicalKey: "L-abcd1234",
		MarkdownBody: "draft",
	}))

	writeIssueFile(t, workspace, filepath.Join("open", "PROJ-3-missing-snapshot.md"), mustRenderDoc(t, issue.Document{
		FrontMatter: issue.FrontMatter{
			SchemaVersion: contracts.IssueFileSchemaVersionV1,
			Key:           "PROJ-3",
			Summary:       "No snapshot",
			IssueType:     "Task",
			Status:        "Open",
		},
		CanonicalKey: "PROJ-3",
		MarkdownBody: "body",
	}))

	writeIssueFile(t, workspace, filepath.Join("open", "PROJ-4-bad-front-matter.md"), "not-front-matter")
	writeIssueFile(t, workspace, filepath.Join("open", "PROJ-5-bad-adf.md"), strings.Join([]string{
		"---",
		"schema_version: \"1\"",
		"key: \"PROJ-5\"",
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
		"",
	}, "\n"))

	report, err := RunStatus(workspace, StatusOptions{State: "all"})
	if err != nil {
		t.Fatalf("run status failed: %v", err)
	}

	if report.Counts.Processed != 5 {
		t.Fatalf("unexpected processed count: %d", report.Counts.Processed)
	}
	if report.Counts.Updated != 1 || report.Counts.Created != 1 || report.Counts.Conflicts != 1 || report.Counts.Errors != 2 {
		t.Fatalf("unexpected counts: %#v", report.Counts)
	}

	byKey := make(map[string]contracts.PerIssueResult, len(report.Issues))
	for _, item := range report.Issues {
		byKey[item.Key] = item
	}

	if _, exists := byKey["PROJ-1"]; exists {
		t.Fatalf("unchanged issue should be excluded by default")
	}

	if got := byKey["PROJ-2"]; got.Action != "modified" || got.Status != contracts.PerIssueStatusSuccess {
		t.Fatalf("expected PROJ-2 modified success, got %#v", got)
	}

	if got := byKey["L-abcd1234"]; got.Action != "new" || got.Status != contracts.PerIssueStatusSuccess {
		t.Fatalf("expected local draft to be marked new, got %#v", got)
	}

	if got := byKey["PROJ-3"]; got.Action != "local-conflict" || got.Status != contracts.PerIssueStatusConflict || len(got.Messages) == 0 || got.Messages[0].ReasonCode != contracts.ReasonCodeConflictBaseSnapshotMissing {
		t.Fatalf("expected PROJ-3 local-conflict with base snapshot reason, got %#v", got)
	}

	if got := byKey["PROJ-4"]; got.Status != contracts.PerIssueStatusError || len(got.Messages) == 0 || got.Messages[0].ReasonCode != contracts.ReasonCodeValidationFailed {
		t.Fatalf("expected malformed front matter typed error, got %#v", got)
	}

	if got := byKey["PROJ-5"]; got.Status != contracts.PerIssueStatusError || len(got.Messages) == 0 || got.Messages[0].ReasonCode != contracts.ReasonCodeDescriptionADFBlockMalformed {
		t.Fatalf("expected malformed ADF typed error, got %#v", got)
	}
}

func TestRunDiffProducesDeterministicOutput(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()

	local := mustRenderDoc(t, issue.Document{
		FrontMatter: issue.FrontMatter{
			SchemaVersion: contracts.IssueFileSchemaVersionV1,
			Key:           "PROJ-9",
			Summary:       "New Summary",
			IssueType:     "Task",
			Status:        "Open",
		},
		CanonicalKey: "PROJ-9",
		MarkdownBody: "new-body",
	})
	original := mustRenderDoc(t, issue.Document{
		FrontMatter: issue.FrontMatter{
			SchemaVersion: contracts.IssueFileSchemaVersionV1,
			Key:           "PROJ-9",
			Summary:       "Old Summary",
			IssueType:     "Task",
			Status:        "Open",
		},
		CanonicalKey: "PROJ-9",
		MarkdownBody: "old-body",
	})

	writeIssueFile(t, workspace, filepath.Join("open", "PROJ-9-diff.md"), local)
	writeIssueFile(t, workspace, filepath.Join(".sync", "originals", "PROJ-9.md"), original)

	reportA, err := RunDiff(workspace, DiffOptions{State: "all"})
	if err != nil {
		t.Fatalf("run diff A failed: %v", err)
	}
	reportB, err := RunDiff(workspace, DiffOptions{State: "all"})
	if err != nil {
		t.Fatalf("run diff B failed: %v", err)
	}

	if len(reportA.Issues) != 1 || len(reportA.Issues[0].Messages) != 1 {
		t.Fatalf("unexpected diff payload: %#v", reportA)
	}
	if reportA.Issues[0].Action != "different" {
		t.Fatalf("expected different action, got %#v", reportA.Issues[0])
	}

	diffA := reportA.Issues[0].Messages[0].Text
	diffB := reportB.Issues[0].Messages[0].Text
	if diffA != diffB {
		t.Fatalf("diff output is not deterministic")
	}
	if !strings.Contains(diffA, "--- original\n+++ local") {
		t.Fatalf("missing deterministic diff headers: %q", diffA)
	}
	if !strings.Contains(diffA, "- summary: \"Old Summary\"") || !strings.Contains(diffA, "+ summary: \"New Summary\"") {
		t.Fatalf("expected summary lines in diff: %q", diffA)
	}
}

func TestRunListSupportsDeterministicFiltering(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()

	writeIssueFile(t, workspace, filepath.Join("open", "PROJ-2-open.md"), mustRenderDoc(t, issue.Document{
		FrontMatter: issue.FrontMatter{
			SchemaVersion: contracts.IssueFileSchemaVersionV1,
			Key:           "PROJ-2",
			Summary:       "Open issue",
			IssueType:     "Task",
			Status:        "Open",
		},
		CanonicalKey: "PROJ-2",
	}))
	writeIssueFile(t, workspace, filepath.Join("closed", "PROJ-1-closed.md"), mustRenderDoc(t, issue.Document{
		FrontMatter: issue.FrontMatter{
			SchemaVersion: contracts.IssueFileSchemaVersionV1,
			Key:           "PROJ-1",
			Summary:       "Closed issue",
			IssueType:     "Task",
			Status:        "Done",
		},
		CanonicalKey: "PROJ-1",
	}))

	closedOnly, err := RunList(workspace, ListOptions{State: "closed"})
	if err != nil {
		t.Fatalf("run list closed failed: %v", err)
	}
	if len(closedOnly.Issues) != 1 || closedOnly.Issues[0].Key != "PROJ-1" {
		t.Fatalf("expected only closed issue, got %#v", closedOnly.Issues)
	}

	keyFiltered, err := RunList(workspace, ListOptions{State: "all", Key: "proj-2"})
	if err != nil {
		t.Fatalf("run list key filter failed: %v", err)
	}
	if len(keyFiltered.Issues) != 1 || keyFiltered.Issues[0].Key != "PROJ-2" {
		t.Fatalf("expected key filter to match PROJ-2, got %#v", keyFiltered.Issues)
	}

	if _, err := RunList(workspace, ListOptions{State: "invalid"}); err == nil {
		t.Fatalf("expected invalid state filter error")
	}
}

func mustRenderDoc(t *testing.T, doc issue.Document) string {
	t.Helper()

	rendered, err := issue.RenderDocument(doc)
	if err != nil {
		t.Fatalf("render document failed: %v", err)
	}
	return rendered
}

func writeIssueFile(t *testing.T, workspace string, relativePath string, content string) {
	t.Helper()

	path := filepath.Join(workspace, ".issues", relativePath)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write file failed: %v", err)
	}
}
