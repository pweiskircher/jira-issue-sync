package commands

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/pat/jira-issue-sync/internal/config"
	"github.com/pat/jira-issue-sync/internal/contracts"
	"github.com/pat/jira-issue-sync/internal/jira"
	"github.com/pat/jira-issue-sync/internal/store"
)

func TestRunPullContinuesAfterPerIssueFailures(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	writePullConfig(t, workspace)

	adapter := &pullAdapterStub{}
	adapter.search = func(ctx context.Context, request jira.SearchIssuesRequest) (jira.SearchIssuesResponse, error) {
		switch request.StartAt {
		case 0:
			return jira.SearchIssuesResponse{StartAt: 0, Total: 2, Issues: []jira.Issue{{
				Key: "PROJ-1",
				Fields: jira.IssueFields{
					Summary:     "First",
					Description: json.RawMessage(`{"version":1,"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"ok"}]}]}`),
					Status:      &jira.StatusRef{Name: "Open"},
					IssueType:   &jira.NamedRef{Name: "Task"},
					UpdatedAt:   "2026-02-20T12:00:00Z",
				},
			}}}, nil
		case 1:
			return jira.SearchIssuesResponse{StartAt: 1, Total: 2, Issues: []jira.Issue{{
				Key: "PROJ-2",
				Fields: jira.IssueFields{
					Summary:     "Second",
					Description: json.RawMessage(`{"version":1,"type":"doc","content":[}`),
					Status:      &jira.StatusRef{Name: "Done"},
					IssueType:   &jira.NamedRef{Name: "Task"},
				},
			}}}, nil
		default:
			return jira.SearchIssuesResponse{StartAt: request.StartAt, Total: 2}, nil
		}
	}

	report, err := RunPull(context.Background(), workspace, PullOptions{
		PageSize:    1,
		Adapter:     adapter,
		Environment: config.Environment{JiraAPIToken: "token"},
	})
	if err != nil {
		t.Fatalf("run pull failed: %v", err)
	}

	if len(adapter.requests) != 2 {
		t.Fatalf("expected paginated search requests, got %d", len(adapter.requests))
	}
	if report.Counts.Processed != 2 || report.Counts.Updated != 1 || report.Counts.Errors != 1 {
		t.Fatalf("unexpected pull counts: %#v", report.Counts)
	}

	if _, err := os.Stat(filepath.Join(workspace, contracts.DefaultOpenDir, "PROJ-1-first.md")); err != nil {
		t.Fatalf("expected pulled file, got %v", err)
	}
	if _, err := os.Stat(filepath.Join(workspace, contracts.DefaultOriginalsDir, "PROJ-1.md")); err != nil {
		t.Fatalf("expected original snapshot, got %v", err)
	}

	issueStore, err := store.New(filepath.Join(workspace, contracts.DefaultIssuesRootDir))
	if err != nil {
		t.Fatalf("store init failed: %v", err)
	}
	cache, err := issueStore.LoadCache()
	if err != nil {
		t.Fatalf("load cache failed: %v", err)
	}
	if _, ok := cache.Issues["PROJ-1"]; !ok {
		t.Fatalf("expected PROJ-1 in cache")
	}
	if _, ok := cache.Issues["PROJ-2"]; ok {
		t.Fatalf("did not expect failed issue in cache")
	}
}

func writePullConfig(t *testing.T, workspace string) {
	t.Helper()

	cfg := contracts.Config{
		ConfigVersion: contracts.ConfigSchemaVersionV1,
		Profiles: map[string]contracts.ProjectProfile{
			"default": {
				ProjectKey: "PROJ",
				DefaultJQL: "project = PROJ",
			},
		},
	}

	if err := config.Write(filepath.Join(workspace, contracts.DefaultConfigFilePath), cfg); err != nil {
		t.Fatalf("write config failed: %v", err)
	}
}

type pullAdapterStub struct {
	requests []jira.SearchIssuesRequest
	search   func(context.Context, jira.SearchIssuesRequest) (jira.SearchIssuesResponse, error)
}

func (s *pullAdapterStub) SearchIssues(ctx context.Context, request jira.SearchIssuesRequest) (jira.SearchIssuesResponse, error) {
	s.requests = append(s.requests, request)
	if s.search == nil {
		return jira.SearchIssuesResponse{}, nil
	}
	return s.search(ctx, request)
}

func (s *pullAdapterStub) GetIssue(context.Context, string, []string) (jira.Issue, error) {
	panic("unexpected call")
}
func (s *pullAdapterStub) CreateIssue(context.Context, jira.CreateIssueRequest) (jira.CreatedIssue, error) {
	panic("unexpected call")
}
func (s *pullAdapterStub) UpdateIssue(context.Context, string, jira.UpdateIssueRequest) error {
	panic("unexpected call")
}
func (s *pullAdapterStub) ListTransitions(context.Context, string) ([]jira.Transition, error) {
	panic("unexpected call")
}
func (s *pullAdapterStub) ApplyTransition(context.Context, string, string) error {
	panic("unexpected call")
}
func (s *pullAdapterStub) ResolveTransition(context.Context, string, contracts.TransitionSelection) (jira.TransitionResolution, error) {
	panic("unexpected call")
}
