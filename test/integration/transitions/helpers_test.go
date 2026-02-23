package transitionsintegration

import (
	"context"
	"errors"
	"os"
	"path/filepath"

	"github.com/pweiskircher/jira-issue-sync/internal/config"
	"github.com/pweiskircher/jira-issue-sync/internal/contracts"
	"github.com/pweiskircher/jira-issue-sync/internal/issue"
	"github.com/pweiskircher/jira-issue-sync/internal/jira"
)

func writeTransitionConfig(t interface {
	Helper()
	Fatalf(string, ...any)
}, workspace string) {
	t.Helper()
	cfg := contracts.Config{
		ConfigVersion: contracts.ConfigSchemaVersionV1,
		Profiles: map[string]contracts.ProjectProfile{
			"team": {
				ProjectKey: "PROJ",
				DefaultJQL: "project = PROJ",
				TransitionOverrides: map[string]contracts.TransitionOverride{
					"Done": {
						TransitionID:   "42",
						TransitionName: "Ship",
						Dynamic: &contracts.DynamicTransitionSelector{
							TargetStatus: "Done",
							Aliases:      []string{"Released"},
						},
					},
				},
			},
		},
		DefaultProfile: "team",
	}
	if err := config.Write(filepath.Join(workspace, contracts.DefaultConfigFilePath), cfg); err != nil {
		t.Fatalf("write config failed: %v", err)
	}
}

func writeTransitionIssueFixtures(t interface {
	Helper()
	Fatalf(string, ...any)
}, workspace string) {
	t.Helper()
	writeTransitionIssueDoc(t, workspace, filepath.Join("open", "PROJ-9-transition.md"), issue.Document{
		CanonicalKey: "PROJ-9",
		FrontMatter: issue.FrontMatter{
			SchemaVersion: contracts.IssueFileSchemaVersionV1,
			Key:           "PROJ-9",
			Summary:       "Local changed",
			IssueType:     "Task",
			Status:        "Done",
		},
		MarkdownBody: "body",
	})
	writeTransitionIssueDoc(t, workspace, filepath.Join(".sync", "originals", "PROJ-9.md"), issue.Document{
		CanonicalKey: "PROJ-9",
		FrontMatter: issue.FrontMatter{
			SchemaVersion: contracts.IssueFileSchemaVersionV1,
			Key:           "PROJ-9",
			Summary:       "Remote summary",
			IssueType:     "Task",
			Status:        "To Do",
		},
		MarkdownBody: "body",
	})
}

func writeTransitionIssueDoc(t interface {
	Helper()
	Fatalf(string, ...any)
}, workspace string, relativePath string, doc issue.Document) {
	t.Helper()
	rendered, err := issue.RenderDocument(doc)
	if err != nil {
		t.Fatalf("render document failed: %v", err)
	}
	path := filepath.Join(workspace, contracts.DefaultIssuesRootDir, relativePath)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}
	if err := os.WriteFile(path, []byte(rendered), 0o644); err != nil {
		t.Fatalf("write file failed: %v", err)
	}
}

type transitionAdapterStub struct {
	issues           map[string]jira.Issue
	resolutions      map[string]jira.TransitionResolution
	selectionByIssue map[string]contracts.TransitionSelection
}

func (s *transitionAdapterStub) SearchIssues(context.Context, jira.SearchIssuesRequest) (jira.SearchIssuesResponse, error) {
	return jira.SearchIssuesResponse{}, nil
}

func (s *transitionAdapterStub) ListFields(context.Context) ([]jira.FieldDefinition, error) {
	return nil, nil
}

func (s *transitionAdapterStub) GetIssue(_ context.Context, issueKey string, _ []string) (jira.Issue, error) {
	if issue, ok := s.issues[issueKey]; ok {
		return issue, nil
	}
	return jira.Issue{}, errors.New("missing issue")
}

func (s *transitionAdapterStub) CreateIssue(context.Context, jira.CreateIssueRequest) (jira.CreatedIssue, error) {
	return jira.CreatedIssue{Key: "PROJ-0"}, nil
}

func (s *transitionAdapterStub) UpdateIssue(context.Context, string, jira.UpdateIssueRequest) error {
	return nil
}

func (s *transitionAdapterStub) ListTransitions(context.Context, string) ([]jira.Transition, error) {
	return nil, nil
}

func (s *transitionAdapterStub) ApplyTransition(context.Context, string, string) error {
	return nil
}

func (s *transitionAdapterStub) ResolveTransition(_ context.Context, issueKey string, selection contracts.TransitionSelection) (jira.TransitionResolution, error) {
	if s.selectionByIssue == nil {
		s.selectionByIssue = map[string]contracts.TransitionSelection{}
	}
	s.selectionByIssue[issueKey] = selection
	if resolution, ok := s.resolutions[issueKey]; ok {
		return resolution, nil
	}
	return jira.TransitionResolution{Kind: jira.TransitionResolutionUnavailable, ReasonCode: contracts.ReasonCodeTransitionUnavailable}, nil
}
