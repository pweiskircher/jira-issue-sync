package pull

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/pweiskircher/jira-issue-sync/internal/contracts"
	"github.com/pweiskircher/jira-issue-sync/internal/jira"
	"github.com/pweiskircher/jira-issue-sync/internal/store"
)

type paginationAdapterStub struct {
	requests []jira.SearchIssuesRequest
	search   func(context.Context, jira.SearchIssuesRequest) (jira.SearchIssuesResponse, error)
}

func (s *paginationAdapterStub) SearchIssues(ctx context.Context, request jira.SearchIssuesRequest) (jira.SearchIssuesResponse, error) {
	s.requests = append(s.requests, request)
	return s.search(ctx, request)
}

func (s *paginationAdapterStub) ListFields(context.Context) ([]jira.FieldDefinition, error) {
	panic("unexpected call")
}

func (s *paginationAdapterStub) GetIssue(context.Context, string, []string) (jira.Issue, error) {
	panic("unexpected call")
}
func (s *paginationAdapterStub) CreateIssue(context.Context, jira.CreateIssueRequest) (jira.CreatedIssue, error) {
	panic("unexpected call")
}
func (s *paginationAdapterStub) UpdateIssue(context.Context, string, jira.UpdateIssueRequest) error {
	panic("unexpected call")
}
func (s *paginationAdapterStub) ListTransitions(context.Context, string) ([]jira.Transition, error) {
	panic("unexpected call")
}
func (s *paginationAdapterStub) ApplyTransition(context.Context, string, string) error {
	panic("unexpected call")
}
func (s *paginationAdapterStub) ResolveTransition(context.Context, string, contracts.TransitionSelection) (jira.TransitionResolution, error) {
	panic("unexpected call")
}

func TestIssueStateFromStatusTreatsRejectedAsClosed(t *testing.T) {
	t.Parallel()

	closedStatuses := []string{"Rejected", "Declined", "Cancelled", "Won't Do"}
	for _, status := range closedStatuses {
		if got := issueStateFromStatus(status); got != "closed" {
			t.Fatalf("expected status %q to be closed, got %q", status, got)
		}
	}
}

func TestFetchIssuesUsesTokenPaginationWhenAvailable(t *testing.T) {
	t.Parallel()

	adapter := &paginationAdapterStub{}
	adapter.search = func(_ context.Context, request jira.SearchIssuesRequest) (jira.SearchIssuesResponse, error) {
		switch len(adapter.requests) {
		case 1:
			if request.NextPageToken != "" {
				t.Fatalf("did not expect token on first page: %q", request.NextPageToken)
			}
			return jira.SearchIssuesResponse{
				Issues:        []jira.Issue{{Key: "PROJ-1"}},
				NextPageToken: "token-2",
			}, nil
		case 2:
			if request.NextPageToken != "token-2" {
				t.Fatalf("expected token on second page, got %q", request.NextPageToken)
			}
			return jira.SearchIssuesResponse{
				Issues: []jira.Issue{{Key: "PROJ-2"}},
				IsLast: true,
			}, nil
		default:
			t.Fatalf("unexpected extra request: %#v", request)
			return jira.SearchIssuesResponse{}, nil
		}
	}

	issues, err := fetchIssues(context.Background(), adapter, "project = PROJ", 50, []string{"*navigable"})
	if err != nil {
		t.Fatalf("fetch issues failed: %v", err)
	}
	if len(issues) != 2 {
		t.Fatalf("expected 2 issues, got %d", len(issues))
	}
}

func TestPipelineMarksUnchangedIssueWithoutRewriting(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	issueStore, err := store.New(filepath.Join(root, contracts.DefaultIssuesRootDir))
	if err != nil {
		t.Fatalf("store init failed: %v", err)
	}

	adapter := &paginationAdapterStub{}
	adapter.search = func(_ context.Context, _ jira.SearchIssuesRequest) (jira.SearchIssuesResponse, error) {
		return jira.SearchIssuesResponse{
			StartAt: 0,
			Total:   1,
			Issues: []jira.Issue{{
				Key: "PROJ-1",
				Fields: jira.IssueFields{
					Summary:     "Stable",
					Description: json.RawMessage(`{"version":1,"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"same"}]}]}`),
					Status:      &jira.StatusRef{Name: "Open"},
					IssueType:   &jira.NamedRef{Name: "Task"},
					UpdatedAt:   "2026-02-20T12:00:00Z",
				},
			}},
		}, nil
	}

	now := func() time.Time {
		return time.Date(2026, time.February, 25, 21, 0, 0, 0, time.UTC)
	}

	pipeline := Pipeline{
		Adapter:   adapter,
		Store:     issueStore,
		Converter: NewADFMarkdownConverter(),
		Now:       now,
	}

	first, err := pipeline.Execute(context.Background(), "project = PROJ")
	if err != nil {
		t.Fatalf("first execute failed: %v", err)
	}
	if len(first.Outcomes) != 1 || !first.Outcomes[0].Updated || first.Outcomes[0].Action != "pull" {
		t.Fatalf("unexpected first outcome: %#v", first.Outcomes)
	}

	second, err := pipeline.Execute(context.Background(), "project = PROJ")
	if err != nil {
		t.Fatalf("second execute failed: %v", err)
	}
	if len(second.Outcomes) != 1 {
		t.Fatalf("unexpected second outcomes length: %#v", second.Outcomes)
	}
	if second.Outcomes[0].Updated {
		t.Fatalf("expected unchanged second outcome, got %#v", second.Outcomes[0])
	}
	if second.Outcomes[0].Action != "unchanged" {
		t.Fatalf("expected unchanged action, got %#v", second.Outcomes[0])
	}
}
