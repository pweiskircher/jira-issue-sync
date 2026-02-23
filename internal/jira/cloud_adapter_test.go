package jira

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/pweiskircher/jira-issue-sync/internal/contracts"
	httpclient "github.com/pweiskircher/jira-issue-sync/internal/http"
)

func TestCloudAdapterImplementsAdapterInterface(t *testing.T) {
	t.Parallel()

	var _ Adapter = (*CloudAdapter)(nil)
}

func TestCloudAdapterSearchIssuesRetriesOnDefaultRetryCodes(t *testing.T) {
	t.Parallel()

	attempts := 0
	methods := make([]string, 0)
	paths := make([]string, 0)
	queries := make([]string, 0)
	bodies := make([]string, 0)
	headers := make([]string, 0)
	mu := sync.Mutex{}

	adapter := mustNewCloudAdapter(t, CloudAdapterOptions{
		BaseURL:  "https://example.atlassian.net",
		Email:    "agent@example.com",
		APIToken: "token-123",
		HTTPDoer: doerFunc(func(req *http.Request) (*http.Response, error) {
			payload := []byte{}
			if req.Body != nil {
				body, err := io.ReadAll(req.Body)
				if err != nil {
					return nil, err
				}
				payload = body
			}

			mu.Lock()
			attempts++
			methods = append(methods, req.Method)
			paths = append(paths, req.URL.Path)
			queries = append(queries, req.URL.RawQuery)
			bodies = append(bodies, string(payload))
			headers = append(headers, req.Header.Get("Authorization"))
			currentAttempt := attempts
			mu.Unlock()

			if currentAttempt == 1 {
				return responseWithStatus(http.StatusServiceUnavailable, `{"errorMessages":["busy"]}`), nil
			}
			return responseWithStatus(http.StatusOK, `{
				"startAt": 0,
				"maxResults": 1,
				"total": 1,
				"issues": [
					{
						"id": "101",
						"key": "PROJ-1",
						"fields": {
							"summary": "Issue",
							"labels": ["one"],
							"description": {"type":"doc","version":1,"content":[]},
						"customfield_10010": "Enterprise"
						}
					}
				]
			}`), nil
		}),
	})

	result, err := adapter.SearchIssues(context.Background(), SearchIssuesRequest{
		JQL:        "project = PROJ",
		StartAt:    0,
		MaxResults: 50,
		Fields:     []string{"summary", "labels", "description"},
	})
	if err != nil {
		t.Fatalf("expected search success, got %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if attempts != 2 {
		t.Fatalf("expected 2 attempts with retry, got %d", attempts)
	}
	if result.Total != 1 || len(result.Issues) != 1 || result.Issues[0].Key != "PROJ-1" {
		t.Fatalf("unexpected search result: %#v", result)
	}
	if got := string(result.Issues[0].Fields.CustomFields["customfield_10010"]); got != "\"Enterprise\"" {
		t.Fatalf("expected custom field payload, got %q", got)
	}

	for i := range methods {
		if methods[i] != http.MethodGet {
			t.Fatalf("unexpected method on attempt %d: %s", i+1, methods[i])
		}
		if paths[i] != "/rest/api/3/search/jql" {
			t.Fatalf("unexpected path on attempt %d: %s", i+1, paths[i])
		}
		if !strings.Contains(queries[i], "jql=project+%3D+PROJ") {
			t.Fatalf("missing jql query on attempt %d: %s", i+1, queries[i])
		}
		if !strings.Contains(queries[i], "maxResults=50") {
			t.Fatalf("missing maxResults query on attempt %d: %s", i+1, queries[i])
		}
		if !strings.Contains(queries[i], "fields=summary%2Clabels%2Cdescription") {
			t.Fatalf("missing fields query on attempt %d: %s", i+1, queries[i])
		}
		if strings.TrimSpace(bodies[i]) != "" {
			t.Fatalf("unexpected payload on attempt %d: %s", i+1, bodies[i])
		}
		if !strings.HasPrefix(headers[i], "Basic ") {
			t.Fatalf("missing basic auth header on attempt %d", i+1)
		}
	}
}

func TestCloudAdapterCRUDAndTransitionEndpoints(t *testing.T) {
	t.Parallel()

	token := "token-xyz"
	email := "agent@example.com"
	authHeader := "Basic " + base64.StdEncoding.EncodeToString([]byte(email+":"+token))

	var gotCreateBody string
	var gotUpdateBody string
	var gotApplyTransitionBody string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.Header.Get("Authorization") != authHeader {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		switch {
		case req.Method == http.MethodGet && req.URL.Path == "/rest/api/3/issue/PROJ-7":
			if req.URL.Query().Get("fields") != "summary,status" {
				http.Error(w, "missing fields query", http.StatusBadRequest)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
				"id": "7",
				"key": "PROJ-7",
				"fields": {
					"summary": "Current",
					"status": {"id":"3","name":"Done"},
					"labels": ["alpha", "beta"],
					"description": {"type":"doc","version":1,"content":[]}
				}
			}`))
		case req.Method == http.MethodPost && req.URL.Path == "/rest/api/3/issue":
			payload, _ := io.ReadAll(req.Body)
			gotCreateBody = string(payload)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":"99","key":"PROJ-99","self":"https://example/rest/api/3/issue/99"}`))
		case req.Method == http.MethodPut && req.URL.Path == "/rest/api/3/issue/PROJ-7":
			payload, _ := io.ReadAll(req.Body)
			gotUpdateBody = string(payload)
			w.WriteHeader(http.StatusNoContent)
		case req.Method == http.MethodGet && req.URL.Path == "/rest/api/3/issue/PROJ-7/transitions":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
				"transitions": [
					{"id":"31","name":"Ship","to":{"id":"6","name":"Released"}},
					{"id":"11","name":"Start Progress","to":{"id":"4","name":"In Progress"}}
				]
			}`))
		case req.Method == http.MethodPost && req.URL.Path == "/rest/api/3/issue/PROJ-7/transitions":
			payload, _ := io.ReadAll(req.Body)
			gotApplyTransitionBody = string(payload)
			w.WriteHeader(http.StatusNoContent)
		default:
			http.Error(w, "unexpected endpoint", http.StatusNotFound)
		}
	}))
	defer server.Close()

	adapter := mustNewCloudAdapter(t, CloudAdapterOptions{
		BaseURL:  server.URL,
		Email:    email,
		APIToken: token,
	})

	issue, err := adapter.GetIssue(context.Background(), "PROJ-7", []string{"summary", "status"})
	if err != nil {
		t.Fatalf("expected get issue success, got %v", err)
	}
	if issue.Key != "PROJ-7" || issue.Fields.Status == nil || issue.Fields.Status.Name != "Done" {
		t.Fatalf("unexpected issue mapping: %#v", issue)
	}

	created, err := adapter.CreateIssue(context.Background(), CreateIssueRequest{
		ProjectKey:        "PROJ",
		IssueTypeName:     "Task",
		Summary:           "Created summary",
		Labels:            []string{"one", "two"},
		AssigneeAccountID: "acc-1",
		PriorityName:      "High",
	})
	if err != nil {
		t.Fatalf("expected create issue success, got %v", err)
	}
	if created.Key != "PROJ-99" {
		t.Fatalf("unexpected create response: %#v", created)
	}
	if !strings.Contains(gotCreateBody, `"project":{"key":"PROJ"}`) || !strings.Contains(gotCreateBody, `"priority":{"name":"High"}`) {
		t.Fatalf("unexpected create payload: %s", gotCreateBody)
	}

	summary := "Updated"
	assignee := ""
	priority := "Highest"
	labels := []string{"alpha"}
	if err := adapter.UpdateIssue(context.Background(), "PROJ-7", UpdateIssueRequest{
		Summary:           &summary,
		AssigneeAccountID: &assignee,
		PriorityName:      &priority,
		Labels:            &labels,
	}); err != nil {
		t.Fatalf("expected update success, got %v", err)
	}
	if !strings.Contains(gotUpdateBody, `"summary":"Updated"`) || !strings.Contains(gotUpdateBody, `"assignee":null`) {
		t.Fatalf("unexpected update payload: %s", gotUpdateBody)
	}

	transitions, err := adapter.ListTransitions(context.Background(), "PROJ-7")
	if err != nil {
		t.Fatalf("expected list transitions success, got %v", err)
	}
	if !reflect.DeepEqual([]string{transitions[0].ID, transitions[1].ID}, []string{"11", "31"}) {
		t.Fatalf("expected deterministic transition order, got %#v", transitions)
	}

	if err := adapter.ApplyTransition(context.Background(), "PROJ-7", "31"); err != nil {
		t.Fatalf("expected apply transition success, got %v", err)
	}
	if gotApplyTransitionBody != `{"transition":{"id":"31"}}` {
		t.Fatalf("unexpected transition payload: %s", gotApplyTransitionBody)
	}
}

func TestCloudAdapterResolveTransitionReturnsTypedAmbiguousOutcome(t *testing.T) {
	t.Parallel()

	adapter := mustNewCloudAdapter(t, CloudAdapterOptions{
		BaseURL:  "https://example.atlassian.net",
		Email:    "agent@example.com",
		APIToken: "token-123",
		HTTPDoer: doerFunc(func(req *http.Request) (*http.Response, error) {
			if req.URL.Path != "/rest/api/3/issue/PROJ-5/transitions" {
				return responseWithStatus(http.StatusNotFound, ""), nil
			}
			return responseWithStatus(http.StatusOK, `{
				"transitions": [
					{"id":"31","name":"Ship","to":{"id":"6","name":"Released"}},
					{"id":"21","name":"Ship","to":{"id":"5","name":"Released"}}
				]
			}`), nil
		}),
	})

	resolution, err := adapter.ResolveTransition(context.Background(), "PROJ-5", contracts.TransitionSelection{
		Kind:           contracts.TransitionSelectionByName,
		TransitionName: "Ship",
	})
	if err != nil {
		t.Fatalf("expected resolve transition success, got %v", err)
	}

	if resolution.Kind != TransitionResolutionAmbiguous {
		t.Fatalf("expected ambiguous resolution, got %#v", resolution)
	}
	if resolution.ReasonCode != contracts.ReasonCodeTransitionAmbiguous {
		t.Fatalf("unexpected reason code: %s", resolution.ReasonCode)
	}
	if len(resolution.Matches) != 2 {
		t.Fatalf("expected 2 matches, got %#v", resolution.Matches)
	}
}

func TestCloudAdapterRedactsSecretsOnTransportAndAuthErrors(t *testing.T) {
	t.Parallel()

	const token = "super-secret-token"
	adapter := mustNewCloudAdapter(t, CloudAdapterOptions{
		BaseURL:  "https://example.atlassian.net",
		Email:    "agent@example.com",
		APIToken: token,
		HTTPDoer: doerFunc(func(req *http.Request) (*http.Response, error) {
			return nil, errors.New("dial failed with token super-secret-token")
		}),
	})

	_, err := adapter.SearchIssues(context.Background(), SearchIssuesRequest{JQL: "project = PROJ"})
	if err == nil {
		t.Fatalf("expected transport error")
	}
	if strings.Contains(err.Error(), token) {
		t.Fatalf("transport error leaked secret: %q", err)
	}
	if !strings.Contains(err.Error(), httpclient.RedactedPlaceholder) {
		t.Fatalf("transport error should include redaction placeholder: %q", err)
	}

	authAdapter := mustNewCloudAdapter(t, CloudAdapterOptions{
		BaseURL:  "https://example.atlassian.net",
		Email:    "agent@example.com",
		APIToken: token,
		HTTPDoer: doerFunc(func(req *http.Request) (*http.Response, error) {
			return responseWithStatus(http.StatusUnauthorized, `{"errorMessages":["invalid super-secret-token"]}`), nil
		}),
	})

	_, err = authAdapter.GetIssue(context.Background(), "PROJ-1", nil)
	if err == nil {
		t.Fatalf("expected auth error")
	}

	var jiraErr *Error
	if !errors.As(err, &jiraErr) {
		t.Fatalf("expected typed jira error, got %T", err)
	}
	if jiraErr.Code != ErrorCodeAuthFailed || jiraErr.ReasonCode != contracts.ReasonCodeAuthFailed {
		t.Fatalf("unexpected auth error classification: %#v", jiraErr)
	}
	if strings.Contains(err.Error(), token) {
		t.Fatalf("auth error leaked secret: %q", err)
	}
}

func TestNewCloudAdapterValidatesRequiredFields(t *testing.T) {
	t.Parallel()

	_, err := NewCloudAdapter(CloudAdapterOptions{BaseURL: "https://example", APIToken: "token", Email: ""})
	if err == nil {
		t.Fatalf("expected validation error for missing email")
	}
	if !IsErrorCode(err, ErrorCodeInvalidInput) {
		t.Fatalf("expected invalid input error, got %v", err)
	}

	_, err = NewCloudAdapter(CloudAdapterOptions{BaseURL: "not-a-url", APIToken: "token", Email: "user@example.com"})
	if err == nil {
		t.Fatalf("expected invalid base URL error")
	}
	if !IsErrorCode(err, ErrorCodeInvalidInput) {
		t.Fatalf("expected invalid input error for base URL, got %v", err)
	}
}

func mustNewCloudAdapter(t *testing.T, options CloudAdapterOptions) *CloudAdapter {
	t.Helper()

	adapter, err := NewCloudAdapter(options)
	if err != nil {
		t.Fatalf("failed to construct adapter: %v", err)
	}
	return adapter
}

type doerFunc func(req *http.Request) (*http.Response, error)

func (f doerFunc) Do(req *http.Request) (*http.Response, error) {
	return f(req)
}

func responseWithStatus(statusCode int, body string) *http.Response {
	return &http.Response{
		StatusCode: statusCode,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func TestCloudAdapterStatusErrorsAreJSONMessageAware(t *testing.T) {
	t.Parallel()

	adapter := mustNewCloudAdapter(t, CloudAdapterOptions{
		BaseURL:  "https://example.atlassian.net",
		Email:    "agent@example.com",
		APIToken: "token-123",
		HTTPDoer: doerFunc(func(req *http.Request) (*http.Response, error) {
			return responseWithStatus(http.StatusBadRequest, `{"errorMessages":["bad request"],"errors":{"summary":"required"}}`), nil
		}),
	})

	_, err := adapter.CreateIssue(context.Background(), CreateIssueRequest{
		ProjectKey:    "PROJ",
		IssueTypeName: "Task",
		Summary:       "demo",
	})
	if err == nil {
		t.Fatalf("expected create failure")
	}

	if !strings.Contains(err.Error(), "bad request") || !strings.Contains(err.Error(), "summary: required") {
		t.Fatalf("expected combined error detail, got %q", err)
	}
}

func TestCloudAdapterRequestPayloadsRemainValidJSON(t *testing.T) {
	t.Parallel()

	description := json.RawMessage(`{"type":"doc","version":1,"content":[]}`)
	adapter := mustNewCloudAdapter(t, CloudAdapterOptions{
		BaseURL:  "https://example.atlassian.net",
		Email:    "agent@example.com",
		APIToken: "token-123",
		HTTPDoer: doerFunc(func(req *http.Request) (*http.Response, error) {
			payload, err := io.ReadAll(req.Body)
			if err != nil {
				return nil, err
			}
			if !json.Valid(payload) {
				return nil, errors.New("invalid json payload")
			}
			return responseWithStatus(http.StatusCreated, `{"id":"1","key":"PROJ-1"}`), nil
		}),
	})

	_, err := adapter.CreateIssue(context.Background(), CreateIssueRequest{
		ProjectKey:    "PROJ",
		IssueTypeName: "Task",
		Summary:       "demo",
		Description:   description,
	})
	if err != nil {
		t.Fatalf("expected valid JSON payload, got %v", err)
	}
}
