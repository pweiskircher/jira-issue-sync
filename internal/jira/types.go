package jira

import (
	"context"
	"encoding/json"

	"github.com/pweiskircher/jira-issue-sync/internal/contracts"
)

type Adapter interface {
	SearchIssues(ctx context.Context, request SearchIssuesRequest) (SearchIssuesResponse, error)
	ListFields(ctx context.Context) ([]FieldDefinition, error)
	GetIssue(ctx context.Context, issueKey string, fields []string) (Issue, error)
	CreateIssue(ctx context.Context, request CreateIssueRequest) (CreatedIssue, error)
	UpdateIssue(ctx context.Context, issueKey string, request UpdateIssueRequest) error
	ListTransitions(ctx context.Context, issueKey string) ([]Transition, error)
	ApplyTransition(ctx context.Context, issueKey string, transitionID string) error
	ResolveTransition(ctx context.Context, issueKey string, selection contracts.TransitionSelection) (TransitionResolution, error)
}

type SearchIssuesRequest struct {
	JQL           string
	StartAt       int
	MaxResults    int
	Fields        []string
	NextPageToken string
}

type SearchIssuesResponse struct {
	StartAt       int
	MaxResults    int
	Total         int
	Issues        []Issue
	NextPageToken string
	IsLast        bool
}

type Issue struct {
	ID     string
	Key    string
	Fields IssueFields
}

type IssueFields struct {
	Summary      string
	Description  json.RawMessage
	Labels       []string
	Assignee     *AccountRef
	Priority     *NamedRef
	Status       *StatusRef
	IssueType    *NamedRef
	Reporter     *AccountRef
	CreatedAt    string
	UpdatedAt    string
	CustomFields map[string]json.RawMessage
}

type AccountRef struct {
	AccountID   string
	DisplayName string
	Email       string
}

type NamedRef struct {
	ID   string
	Name string
}

type StatusRef struct {
	ID   string
	Name string
}

type CreateIssueRequest struct {
	ProjectKey        string
	IssueTypeName     string
	Summary           string
	Description       json.RawMessage
	Labels            []string
	AssigneeAccountID string
	PriorityName      string
}

type CreatedIssue struct {
	ID   string
	Key  string
	Self string
}

type UpdateIssueRequest struct {
	Summary           *string
	Description       *json.RawMessage
	Labels            *[]string
	AssigneeAccountID *string
	PriorityName      *string
}

type Transition struct {
	ID           string
	Name         string
	ToStatusID   string
	ToStatusName string
}

type TransitionResolutionKind string

const (
	TransitionResolutionSelected    TransitionResolutionKind = "selected"
	TransitionResolutionAmbiguous   TransitionResolutionKind = "ambiguous"
	TransitionResolutionUnavailable TransitionResolutionKind = "unavailable"
)

type TransitionResolution struct {
	Kind             TransitionResolutionKind
	SelectionKind    contracts.TransitionSelectionKind
	MatchedCandidate string
	Transition       Transition
	Matches          []Transition
	TriedCandidates  []string
	ReasonCode       contracts.ReasonCode
}

type FieldDefinition struct {
	ID     string
	Name   string
	Custom bool
}
