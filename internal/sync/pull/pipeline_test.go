package pull

import (
	"context"
	"testing"

	"github.com/pat/jira-issue-sync/internal/contracts"
	"github.com/pat/jira-issue-sync/internal/jira"
)

type paginationAdapterStub struct {
	requests []jira.SearchIssuesRequest
	search   func(context.Context, jira.SearchIssuesRequest) (jira.SearchIssuesResponse, error)
}

func (s *paginationAdapterStub) SearchIssues(ctx context.Context, request jira.SearchIssuesRequest) (jira.SearchIssuesResponse, error) {
	s.requests = append(s.requests, request)
	return s.search(ctx, request)
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
				Issues: []jira.Issue{{Key: "PROJ-1"}},
				NextPageToken: "token-2",
			}, nil
		case 2:
			if request.NextPageToken != "token-2" {
				t.Fatalf("expected token on second page, got %q", request.NextPageToken)
			}
			return jira.SearchIssuesResponse{
				Issues:  []jira.Issue{{Key: "PROJ-2"}},
				IsLast:  true,
			}, nil
		default:
			t.Fatalf("unexpected extra request: %#v", request)
			return jira.SearchIssuesResponse{}, nil
		}
	}

	issues, err := fetchIssues(context.Background(), adapter, "project = PROJ", 50)
	if err != nil {
		t.Fatalf("fetch issues failed: %v", err)
	}
	if len(issues) != 2 {
		t.Fatalf("expected 2 issues, got %d", len(issues))
	}
}
