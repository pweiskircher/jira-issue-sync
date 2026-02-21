package jira

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"

	"github.com/pat/jira-issue-sync/internal/contracts"
	httpclient "github.com/pat/jira-issue-sync/internal/http"
)

const maxResponseBodyBytes = 1 << 20

type CloudAdapterOptions struct {
	BaseURL      string
	Email        string
	APIToken     string
	HTTPDoer     httpclient.Doer
	RetryOptions httpclient.Options
}

type CloudAdapter struct {
	baseURL    string
	authHeader string
	client     *httpclient.RetryClient
	redactor   httpclient.Redactor
}

func NewCloudAdapter(options CloudAdapterOptions) (*CloudAdapter, error) {
	baseURL, err := normalizeBaseURL(options.BaseURL)
	if err != nil {
		return nil, err
	}

	email := strings.TrimSpace(options.Email)
	if email == "" {
		return nil, &Error{
			Code:       ErrorCodeInvalidInput,
			ReasonCode: contracts.ReasonCodeValidationFailed,
			Message:    "invalid jira adapter options: email must be set",
		}
	}

	token := strings.TrimSpace(options.APIToken)
	if token == "" {
		return nil, &Error{
			Code:       ErrorCodeInvalidInput,
			ReasonCode: contracts.ReasonCodeValidationFailed,
			Message:    "invalid jira adapter options: api token must be set",
		}
	}

	authSecret := email + ":" + token
	authHeader := "Basic " + base64.StdEncoding.EncodeToString([]byte(authSecret))
	redactor := httpclient.NewRedactor(token, authSecret, authHeader)

	return &CloudAdapter{
		baseURL:    baseURL,
		authHeader: authHeader,
		client:     httpclient.NewRetryClient(options.HTTPDoer, options.RetryOptions),
		redactor:   redactor,
	}, nil
}

func (a *CloudAdapter) SearchIssues(ctx context.Context, request SearchIssuesRequest) (SearchIssuesResponse, error) {
	if a == nil {
		return SearchIssuesResponse{}, &Error{Code: ErrorCodeInvalidInput, Message: "jira adapter is nil"}
	}

	payload := map[string]any{
		"jql": request.JQL,
	}
	if request.StartAt >= 0 {
		payload["startAt"] = request.StartAt
	}
	if request.MaxResults > 0 {
		payload["maxResults"] = request.MaxResults
	}
	if fields := normalizeStringSlice(request.Fields); len(fields) > 0 {
		payload["fields"] = fields
	}

	var response searchIssuesAPIResponse
	if err := a.doJSON(ctx, http.MethodPost, "/rest/api/3/search", nil, payload, []int{http.StatusOK}, &response); err != nil {
		return SearchIssuesResponse{}, err
	}

	issues := make([]Issue, 0, len(response.Issues))
	for _, item := range response.Issues {
		issues = append(issues, mapAPIIssue(item))
	}

	return SearchIssuesResponse{
		StartAt:    response.StartAt,
		MaxResults: response.MaxResults,
		Total:      response.Total,
		Issues:     issues,
	}, nil
}

func (a *CloudAdapter) GetIssue(ctx context.Context, issueKey string, fields []string) (Issue, error) {
	if a == nil {
		return Issue{}, &Error{Code: ErrorCodeInvalidInput, Message: "jira adapter is nil"}
	}

	canonicalKey, err := validateIssueKey(issueKey)
	if err != nil {
		return Issue{}, err
	}

	query := url.Values{}
	if requestedFields := normalizeStringSlice(fields); len(requestedFields) > 0 {
		query.Set("fields", strings.Join(requestedFields, ","))
	}

	resourcePath := "/rest/api/3/issue/" + url.PathEscape(canonicalKey)
	var response issueAPIResponse
	if err := a.doJSON(ctx, http.MethodGet, resourcePath, query, nil, []int{http.StatusOK}, &response); err != nil {
		return Issue{}, err
	}

	return mapAPIIssue(response), nil
}

func (a *CloudAdapter) CreateIssue(ctx context.Context, request CreateIssueRequest) (CreatedIssue, error) {
	if a == nil {
		return CreatedIssue{}, &Error{Code: ErrorCodeInvalidInput, Message: "jira adapter is nil"}
	}

	projectKey := strings.TrimSpace(request.ProjectKey)
	if projectKey == "" {
		return CreatedIssue{}, &Error{
			Code:       ErrorCodeInvalidInput,
			ReasonCode: contracts.ReasonCodeValidationFailed,
			Message:    "invalid create issue request: project key must be set",
		}
	}

	issueTypeName := strings.TrimSpace(request.IssueTypeName)
	if issueTypeName == "" {
		return CreatedIssue{}, &Error{
			Code:       ErrorCodeInvalidInput,
			ReasonCode: contracts.ReasonCodeValidationFailed,
			Message:    "invalid create issue request: issue type name must be set",
		}
	}

	summary := strings.TrimSpace(request.Summary)
	if summary == "" {
		return CreatedIssue{}, &Error{
			Code:       ErrorCodeInvalidInput,
			ReasonCode: contracts.ReasonCodeValidationFailed,
			Message:    "invalid create issue request: summary must be set",
		}
	}

	fields := map[string]any{
		"project":   map[string]string{"key": projectKey},
		"issuetype": map[string]string{"name": issueTypeName},
		"summary":   summary,
	}
	if len(request.Description) > 0 {
		fields["description"] = json.RawMessage(request.Description)
	}
	if labels := normalizeStringSlice(request.Labels); labels != nil {
		fields["labels"] = labels
	}
	if assignee := strings.TrimSpace(request.AssigneeAccountID); assignee != "" {
		fields["assignee"] = map[string]string{"accountId": assignee}
	}
	if priority := strings.TrimSpace(request.PriorityName); priority != "" {
		fields["priority"] = map[string]string{"name": priority}
	}

	payload := map[string]any{"fields": fields}
	var response createdIssueAPIResponse
	if err := a.doJSON(ctx, http.MethodPost, "/rest/api/3/issue", nil, payload, []int{http.StatusCreated}, &response); err != nil {
		return CreatedIssue{}, err
	}

	return CreatedIssue{ID: response.ID, Key: response.Key, Self: response.Self}, nil
}

func (a *CloudAdapter) UpdateIssue(ctx context.Context, issueKey string, request UpdateIssueRequest) error {
	if a == nil {
		return &Error{Code: ErrorCodeInvalidInput, Message: "jira adapter is nil"}
	}

	canonicalKey, err := validateIssueKey(issueKey)
	if err != nil {
		return err
	}

	fields := map[string]any{}
	if request.Summary != nil {
		fields["summary"] = strings.TrimSpace(*request.Summary)
	}
	if request.Description != nil {
		if len(*request.Description) == 0 {
			fields["description"] = nil
		} else {
			fields["description"] = json.RawMessage(*request.Description)
		}
	}
	if request.Labels != nil {
		fields["labels"] = normalizeStringSlice(*request.Labels)
	}
	if request.AssigneeAccountID != nil {
		assignee := strings.TrimSpace(*request.AssigneeAccountID)
		if assignee == "" {
			fields["assignee"] = nil
		} else {
			fields["assignee"] = map[string]string{"accountId": assignee}
		}
	}
	if request.PriorityName != nil {
		priority := strings.TrimSpace(*request.PriorityName)
		if priority == "" {
			fields["priority"] = nil
		} else {
			fields["priority"] = map[string]string{"name": priority}
		}
	}

	if len(fields) == 0 {
		return nil
	}

	resourcePath := "/rest/api/3/issue/" + url.PathEscape(canonicalKey)
	payload := map[string]any{"fields": fields}
	return a.doJSON(ctx, http.MethodPut, resourcePath, nil, payload, []int{http.StatusNoContent}, nil)
}

func (a *CloudAdapter) ListTransitions(ctx context.Context, issueKey string) ([]Transition, error) {
	if a == nil {
		return nil, &Error{Code: ErrorCodeInvalidInput, Message: "jira adapter is nil"}
	}

	canonicalKey, err := validateIssueKey(issueKey)
	if err != nil {
		return nil, err
	}

	resourcePath := "/rest/api/3/issue/" + url.PathEscape(canonicalKey) + "/transitions"
	var response transitionsAPIResponse
	if err := a.doJSON(ctx, http.MethodGet, resourcePath, nil, nil, []int{http.StatusOK}, &response); err != nil {
		return nil, err
	}

	transitions := make([]Transition, 0, len(response.Transitions))
	for _, item := range response.Transitions {
		transitions = append(transitions, Transition{
			ID:           strings.TrimSpace(item.ID),
			Name:         strings.TrimSpace(item.Name),
			ToStatusID:   strings.TrimSpace(item.To.ID),
			ToStatusName: strings.TrimSpace(item.To.Name),
		})
	}

	return sortedTransitionCopy(transitions), nil
}

func (a *CloudAdapter) ApplyTransition(ctx context.Context, issueKey string, transitionID string) error {
	if a == nil {
		return &Error{Code: ErrorCodeInvalidInput, Message: "jira adapter is nil"}
	}

	canonicalKey, err := validateIssueKey(issueKey)
	if err != nil {
		return err
	}

	candidateID := strings.TrimSpace(transitionID)
	if candidateID == "" {
		return &Error{
			Code:       ErrorCodeInvalidInput,
			ReasonCode: contracts.ReasonCodeValidationFailed,
			Message:    "invalid transition request: transition ID must be set",
			redactor:   a.redactor,
		}
	}

	resourcePath := "/rest/api/3/issue/" + url.PathEscape(canonicalKey) + "/transitions"
	payload := map[string]any{
		"transition": map[string]string{"id": candidateID},
	}
	return a.doJSON(ctx, http.MethodPost, resourcePath, nil, payload, []int{http.StatusNoContent}, nil)
}

func (a *CloudAdapter) ResolveTransition(ctx context.Context, issueKey string, selection contracts.TransitionSelection) (TransitionResolution, error) {
	transitions, err := a.ListTransitions(ctx, issueKey)
	if err != nil {
		return TransitionResolution{}, err
	}
	return resolveTransitionSelection(transitions, selection), nil
}

func (a *CloudAdapter) doJSON(ctx context.Context, method string, resourcePath string, query url.Values, payload any, expectedStatusCodes []int, out any) error {
	if len(expectedStatusCodes) == 0 {
		expectedStatusCodes = []int{http.StatusOK}
	}

	var requestBody io.Reader
	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil {
			return &Error{
				Code:       ErrorCodeRequestEncode,
				ReasonCode: contracts.ReasonCodeValidationFailed,
				Message:    "failed to encode jira request payload",
				Err:        err,
				redactor:   a.redactor,
			}
		}
		requestBody = bytes.NewReader(encoded)
	}

	endpoint, err := a.endpointFor(resourcePath, query)
	if err != nil {
		return &Error{
			Code:       ErrorCodeRequestBuild,
			ReasonCode: contracts.ReasonCodeValidationFailed,
			Message:    "failed to build jira request URL",
			Err:        err,
			redactor:   a.redactor,
		}
	}

	req, err := http.NewRequestWithContext(ctx, method, endpoint, requestBody)
	if err != nil {
		return &Error{
			Code:       ErrorCodeRequestBuild,
			ReasonCode: contracts.ReasonCodeValidationFailed,
			Message:    "failed to build jira request",
			Err:        err,
			redactor:   a.redactor,
		}
	}

	req.Header.Set("Accept", "application/json")
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Authorization", a.authHeader)

	resp, err := a.client.Do(req)
	if err != nil {
		return &Error{
			Code:       ErrorCodeTransport,
			ReasonCode: contracts.ReasonCodeTransportError,
			Message:    "failed to execute jira request",
			Err:        err,
			redactor:   a.redactor,
		}
	}
	defer resp.Body.Close()

	responseBody, readErr := io.ReadAll(io.LimitReader(resp.Body, maxResponseBodyBytes))
	if readErr != nil {
		return &Error{
			Code:       ErrorCodeTransport,
			ReasonCode: contracts.ReasonCodeTransportError,
			StatusCode: resp.StatusCode,
			Message:    "failed to read jira response body",
			Err:        readErr,
			redactor:   a.redactor,
		}
	}

	if !containsStatus(expectedStatusCodes, resp.StatusCode) {
		return a.statusError(resp.StatusCode, responseBody)
	}

	if out == nil || len(responseBody) == 0 {
		return nil
	}

	if err := json.Unmarshal(responseBody, out); err != nil {
		return &Error{
			Code:       ErrorCodeResponseDecode,
			ReasonCode: contracts.ReasonCodeTransportError,
			StatusCode: resp.StatusCode,
			Message:    "failed to decode jira response body",
			Err:        err,
			redactor:   a.redactor,
		}
	}

	return nil
}

func (a *CloudAdapter) statusError(statusCode int, body []byte) error {
	detail := extractAPIErrorMessage(body)
	if detail == "" {
		detail = strings.ToLower(http.StatusText(statusCode))
	}

	if statusCode == http.StatusUnauthorized || statusCode == http.StatusForbidden {
		return &Error{
			Code:       ErrorCodeAuthFailed,
			ReasonCode: contracts.ReasonCodeAuthFailed,
			StatusCode: statusCode,
			Message:    fmt.Sprintf("jira authentication failed with status %d: %s", statusCode, detail),
			redactor:   a.redactor,
		}
	}

	return &Error{
		Code:       ErrorCodeUnexpectedStatus,
		ReasonCode: contracts.ReasonCodeTransportError,
		StatusCode: statusCode,
		Message:    fmt.Sprintf("jira request failed with status %d: %s", statusCode, detail),
		redactor:   a.redactor,
	}
}

func (a *CloudAdapter) endpointFor(resourcePath string, query url.Values) (string, error) {
	trimmedPath := "/" + strings.TrimLeft(strings.TrimSpace(resourcePath), "/")
	parsedBase, err := url.Parse(a.baseURL)
	if err != nil {
		return "", err
	}

	parsedBase.Path = strings.TrimRight(parsedBase.Path, "/") + trimmedPath
	if len(query) > 0 {
		parsedBase.RawQuery = query.Encode()
	}
	return parsedBase.String(), nil
}

func normalizeBaseURL(baseURL string) (string, error) {
	trimmed := strings.TrimSpace(baseURL)
	if trimmed == "" {
		return "", &Error{
			Code:       ErrorCodeInvalidInput,
			ReasonCode: contracts.ReasonCodeValidationFailed,
			Message:    "invalid jira adapter options: base URL must be set",
		}
	}

	parsed, err := url.Parse(trimmed)
	if err != nil {
		return "", &Error{
			Code:       ErrorCodeInvalidInput,
			ReasonCode: contracts.ReasonCodeValidationFailed,
			Message:    "invalid jira adapter options: base URL is malformed",
			Err:        err,
		}
	}

	if parsed.Scheme == "" || parsed.Host == "" {
		return "", &Error{
			Code:       ErrorCodeInvalidInput,
			ReasonCode: contracts.ReasonCodeValidationFailed,
			Message:    "invalid jira adapter options: base URL must include scheme and host",
		}
	}

	parsed.Path = strings.TrimRight(parsed.Path, "/")
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String(), nil
}

func validateIssueKey(issueKey string) (string, error) {
	canonicalKey := strings.TrimSpace(issueKey)
	if !contracts.JiraIssueKeyPattern.MatchString(canonicalKey) {
		return "", &Error{
			Code:       ErrorCodeInvalidInput,
			ReasonCode: contracts.ReasonCodeValidationFailed,
			Message:    "invalid issue key",
		}
	}
	return canonicalKey, nil
}

func containsStatus(statuses []int, candidate int) bool {
	for _, status := range statuses {
		if status == candidate {
			return true
		}
	}
	return false
}

func normalizeStringSlice(values []string) []string {
	if values == nil {
		return nil
	}

	normalized := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		normalized = append(normalized, trimmed)
	}
	return normalized
}

func extractAPIErrorMessage(body []byte) string {
	trimmed := strings.TrimSpace(string(body))
	if trimmed == "" {
		return ""
	}

	var payload struct {
		ErrorMessages []string          `json:"errorMessages"`
		Errors        map[string]string `json:"errors"`
		Message       string            `json:"message"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return trimmed
	}

	parts := make([]string, 0, len(payload.ErrorMessages)+len(payload.Errors)+1)
	for _, message := range payload.ErrorMessages {
		message = strings.TrimSpace(message)
		if message != "" {
			parts = append(parts, message)
		}
	}

	if message := strings.TrimSpace(payload.Message); message != "" {
		parts = append(parts, message)
	}

	if len(payload.Errors) > 0 {
		keys := make([]string, 0, len(payload.Errors))
		for key := range payload.Errors {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			value := strings.TrimSpace(payload.Errors[key])
			if value == "" {
				continue
			}
			parts = append(parts, fmt.Sprintf("%s: %s", key, value))
		}
	}

	if len(parts) == 0 {
		return trimmed
	}
	return strings.Join(parts, "; ")
}

type searchIssuesAPIResponse struct {
	StartAt    int                `json:"startAt"`
	MaxResults int                `json:"maxResults"`
	Total      int                `json:"total"`
	Issues     []issueAPIResponse `json:"issues"`
}

type issueAPIResponse struct {
	ID     string             `json:"id"`
	Key    string             `json:"key"`
	Fields issueFieldsAPIData `json:"fields"`
}

type issueFieldsAPIData struct {
	Summary     string          `json:"summary"`
	Description json.RawMessage `json:"description"`
	Labels      []string        `json:"labels"`
	Assignee    *accountAPIRef  `json:"assignee"`
	Priority    *namedAPIRef    `json:"priority"`
	Status      *namedAPIRef    `json:"status"`
	IssueType   *namedAPIRef    `json:"issuetype"`
	Reporter    *accountAPIRef  `json:"reporter"`
	CreatedAt   string          `json:"created"`
	UpdatedAt   string          `json:"updated"`
}

type accountAPIRef struct {
	AccountID   string `json:"accountId"`
	DisplayName string `json:"displayName"`
	Email       string `json:"emailAddress"`
}

type namedAPIRef struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type createdIssueAPIResponse struct {
	ID   string `json:"id"`
	Key  string `json:"key"`
	Self string `json:"self"`
}

type transitionsAPIResponse struct {
	Transitions []transitionAPIData `json:"transitions"`
}

type transitionAPIData struct {
	ID   string      `json:"id"`
	Name string      `json:"name"`
	To   namedAPIRef `json:"to"`
}

func mapAPIIssue(raw issueAPIResponse) Issue {
	return Issue{
		ID:  strings.TrimSpace(raw.ID),
		Key: strings.TrimSpace(raw.Key),
		Fields: IssueFields{
			Summary:     strings.TrimSpace(raw.Fields.Summary),
			Description: cloneRawJSON(raw.Fields.Description),
			Labels:      normalizeStringSlice(raw.Fields.Labels),
			Assignee:    mapAccountRef(raw.Fields.Assignee),
			Priority:    mapNamedRef(raw.Fields.Priority),
			Status:      mapStatusRef(raw.Fields.Status),
			IssueType:   mapNamedRef(raw.Fields.IssueType),
			Reporter:    mapAccountRef(raw.Fields.Reporter),
			CreatedAt:   strings.TrimSpace(raw.Fields.CreatedAt),
			UpdatedAt:   strings.TrimSpace(raw.Fields.UpdatedAt),
		},
	}
}

func mapAccountRef(raw *accountAPIRef) *AccountRef {
	if raw == nil {
		return nil
	}
	return &AccountRef{
		AccountID:   strings.TrimSpace(raw.AccountID),
		DisplayName: strings.TrimSpace(raw.DisplayName),
		Email:       strings.TrimSpace(raw.Email),
	}
}

func mapNamedRef(raw *namedAPIRef) *NamedRef {
	if raw == nil {
		return nil
	}
	return &NamedRef{ID: strings.TrimSpace(raw.ID), Name: strings.TrimSpace(raw.Name)}
}

func mapStatusRef(raw *namedAPIRef) *StatusRef {
	if raw == nil {
		return nil
	}
	return &StatusRef{ID: strings.TrimSpace(raw.ID), Name: strings.TrimSpace(raw.Name)}
}

func cloneRawJSON(value json.RawMessage) json.RawMessage {
	if len(value) == 0 {
		return nil
	}
	return append(json.RawMessage(nil), value...)
}
