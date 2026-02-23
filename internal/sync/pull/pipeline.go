package pull

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/pat/jira-issue-sync/internal/contracts"
	"github.com/pat/jira-issue-sync/internal/converter"
	"github.com/pat/jira-issue-sync/internal/issue"
	"github.com/pat/jira-issue-sync/internal/jira"
	"github.com/pat/jira-issue-sync/internal/store"
)

var pullFields = []string{"summary", "description", "labels", "assignee", "priority", "status", "issuetype", "reporter", "created", "updated"}

type Pipeline struct {
	Adapter     jira.Adapter
	Store       *store.Store
	Converter   converter.Adapter
	PageSize    int
	Concurrency int
	Now         func() time.Time
}

type Outcome struct {
	Key      string
	Action   string
	Status   contracts.PerIssueStatus
	Messages []contracts.IssueMessage
	Updated  bool
}

type Result struct {
	Outcomes []Outcome
	Cache    store.Cache
}

type preparedIssue struct {
	key             string
	summary         string
	canonical       string
	state           store.IssueState
	remoteUpdatedAt string
	err             error
	reasonCode      contracts.ReasonCode
	errorCode       string
}

func (p Pipeline) Execute(ctx context.Context, jql string) (Result, error) {
	if p.Adapter == nil {
		return Result{}, fmt.Errorf("pull adapter is not configured")
	}
	if p.Store == nil {
		return Result{}, fmt.Errorf("pull store is not configured")
	}
	if p.Converter == nil {
		return Result{}, fmt.Errorf("pull converter is not configured")
	}

	trimmedJQL := strings.TrimSpace(jql)
	if trimmedJQL == "" {
		return Result{}, fmt.Errorf("jql is required")
	}

	pageSize := p.PageSize
	if pageSize <= 0 {
		pageSize = contracts.DefaultPullPageSize
	}

	concurrency := p.Concurrency
	if concurrency <= 0 {
		concurrency = contracts.DefaultPullConcurrency
	}

	now := p.Now
	if now == nil {
		now = time.Now
	}

	fetched, err := fetchIssues(ctx, p.Adapter, trimmedJQL, pageSize)
	if err != nil {
		return Result{}, err
	}
	if len(fetched) == 0 {
		cache, cacheErr := p.Store.LoadCache()
		if cacheErr != nil {
			return Result{}, cacheErr
		}
		return Result{Cache: cache}, nil
	}

	sort.Slice(fetched, func(i int, j int) bool {
		return fetched[i].Key < fetched[j].Key
	})

	prepared := prepareIssues(fetched, concurrency, now().UTC(), p.Converter)
	sort.Slice(prepared, func(i int, j int) bool {
		return prepared[i].key < prepared[j].key
	})

	cache, persisted, err := p.persist(prepared)
	if err != nil {
		return Result{}, err
	}

	prepared = persisted

	outcomes := make([]Outcome, 0, len(prepared))
	for _, entry := range prepared {
		if entry.err != nil {
			outcomes = append(outcomes, Outcome{
				Key:    entry.key,
				Action: "pull-error",
				Status: contracts.PerIssueStatusError,
				Messages: []contracts.IssueMessage{{
					Level:      "error",
					ReasonCode: entry.reasonCode,
					Text:       formatIssueError(entry.errorCode, entry.err),
				}},
			})
			continue
		}

		outcomes = append(outcomes, Outcome{
			Key:     entry.key,
			Action:  "pull",
			Status:  contracts.PerIssueStatusSuccess,
			Updated: true,
			Messages: []contracts.IssueMessage{{
				Level: "info",
				Text:  "synchronized issue snapshot",
			}},
		})
	}

	return Result{Outcomes: outcomes, Cache: cache}, nil
}

func (p Pipeline) persist(prepared []preparedIssue) (store.Cache, []preparedIssue, error) {
	cache, err := p.Store.LoadCache()
	if err != nil {
		return store.Cache{}, nil, err
	}

	for index := range prepared {
		entry := &prepared[index]
		if entry.err != nil {
			continue
		}

		previousPath := ""
		if previous, ok := cache.Issues[entry.key]; ok {
			previousPath = previous.Path
		}

		path, writeErr := p.Store.WriteIssue(entry.state, entry.key, entry.summary, entry.canonical)
		if writeErr != nil {
			entry.err = writeErr
			entry.reasonCode = contracts.ReasonCodeValidationFailed
			entry.errorCode = "write_issue_failed"
			continue
		}

		if _, snapErr := p.Store.WriteOriginalSnapshot(entry.key, entry.canonical); snapErr != nil {
			entry.err = snapErr
			entry.reasonCode = contracts.ReasonCodeValidationFailed
			entry.errorCode = "write_snapshot_failed"
			continue
		}

		if previousPath != "" && previousPath != path {
			if removeErr := p.Store.Remove(previousPath); removeErr != nil {
				entry.err = removeErr
				entry.reasonCode = contracts.ReasonCodeValidationFailed
				entry.errorCode = "cleanup_old_path_failed"
				continue
			}
		}

		cache.Issues[entry.key] = store.CacheEntry{
			Path:            path,
			Status:          string(entry.state),
			RemoteUpdatedAt: entry.remoteUpdatedAt,
		}
	}

	if err := p.Store.SaveCache(cache); err != nil {
		return store.Cache{}, nil, err
	}

	return cache, prepared, nil
}

func fetchIssues(ctx context.Context, adapter jira.Adapter, jql string, pageSize int) ([]jira.Issue, error) {
	issues := make([]jira.Issue, 0)
	startAt := 0
	nextPageToken := ""
	usingTokenPagination := false

	for {
		response, err := adapter.SearchIssues(ctx, jira.SearchIssuesRequest{
			JQL:           jql,
			StartAt:       startAt,
			MaxResults:    pageSize,
			Fields:        pullFields,
			NextPageToken: nextPageToken,
		})
		if err != nil {
			return nil, err
		}

		issues = append(issues, response.Issues...)
		if len(response.Issues) == 0 {
			break
		}

		if response.NextPageToken != "" || response.IsLast {
			usingTokenPagination = true
		}
		if usingTokenPagination {
			if response.IsLast || response.NextPageToken == "" {
				break
			}
			nextPageToken = response.NextPageToken
			continue
		}

		startAt = response.StartAt + len(response.Issues)
		if response.Total > 0 && startAt >= response.Total {
			break
		}
		if response.MaxResults > 0 && len(response.Issues) < response.MaxResults {
			break
		}
	}

	return issues, nil
}

func prepareIssues(issues []jira.Issue, concurrency int, syncedAt time.Time, markdownConverter converter.Adapter) []preparedIssue {
	prepared := make([]preparedIssue, len(issues))
	jobs := make(chan int, len(issues))

	workerCount := concurrency
	if workerCount > len(issues) {
		workerCount = len(issues)
	}
	if workerCount <= 0 {
		workerCount = 1
	}

	var wg sync.WaitGroup
	for worker := 0; worker < workerCount; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for index := range jobs {
				prepared[index] = prepareIssue(issues[index], syncedAt, markdownConverter)
			}
		}()
	}

	for index := range issues {
		jobs <- index
	}
	close(jobs)
	wg.Wait()

	return prepared
}

func prepareIssue(remote jira.Issue, syncedAt time.Time, markdownConverter converter.Adapter) preparedIssue {
	key := strings.TrimSpace(remote.Key)
	if key == "" {
		return preparedIssue{key: remote.Key, err: errors.New("issue key is missing"), reasonCode: contracts.ReasonCodeValidationFailed, errorCode: "missing_key"}
	}

	rawADF := strings.TrimSpace(string(remote.Fields.Description))
	markdownResult, err := markdownConverter.ToMarkdown(rawADF)
	if err != nil {
		reason := contracts.ReasonCodeValidationFailed
		if converterErr := asConverterError(err); converterErr != nil {
			reason = converterErr.ReasonCode
		}
		return preparedIssue{key: key, err: err, reasonCode: reason, errorCode: "adf_to_markdown_failed"}
	}

	canonicalADF := ""
	if rawADF != "" {
		canonicalADF, err = converter.ValidateAndCanonicalizeRawADF(rawADF)
		if err != nil {
			reason := contracts.ReasonCodeValidationFailed
			if converterErr := asConverterError(err); converterErr != nil {
				reason = converterErr.ReasonCode
			}
			return preparedIssue{key: key, err: err, reasonCode: reason, errorCode: "adf_validation_failed"}
		}
	}

	doc := issue.Document{
		CanonicalKey: key,
		FrontMatter: issue.FrontMatter{
			SchemaVersion: contracts.IssueFileSchemaVersionV1,
			Key:           key,
			Summary:       strings.TrimSpace(remote.Fields.Summary),
			IssueType:     namedRefValue(remote.Fields.IssueType),
			Status:        statusValue(remote.Fields.Status),
			Priority:      namedRefValue(remote.Fields.Priority),
			Assignee:      accountRefValue(remote.Fields.Assignee),
			Labels:        append([]string(nil), remote.Fields.Labels...),
			Reporter:      accountRefValue(remote.Fields.Reporter),
			CreatedAt:     strings.TrimSpace(remote.Fields.CreatedAt),
			UpdatedAt:     strings.TrimSpace(remote.Fields.UpdatedAt),
			SyncedAt:      syncedAt.Format(time.RFC3339Nano),
		},
		MarkdownBody: markdownResult.Markdown,
		RawADFJSON:   canonicalADF,
	}

	canonical, renderErr := issue.RenderDocument(doc)
	if renderErr != nil {
		return preparedIssue{key: key, err: renderErr, reasonCode: contracts.ReasonCodeValidationFailed, errorCode: "render_document_failed"}
	}

	return preparedIssue{
		key:             key,
		summary:         doc.FrontMatter.Summary,
		canonical:       canonical,
		state:           issueStateFromStatus(doc.FrontMatter.Status),
		remoteUpdatedAt: doc.FrontMatter.UpdatedAt,
	}
}

func issueStateFromStatus(status string) store.IssueState {
	normalized := strings.ToLower(strings.TrimSpace(status))
	switch normalized {
	case "done", "closed", "resolved", "complete", "completed":
		return store.IssueStateClosed
	default:
		return store.IssueStateOpen
	}
}

func namedRefValue(ref *jira.NamedRef) string {
	if ref == nil {
		return ""
	}
	return strings.TrimSpace(ref.Name)
}

func statusValue(ref *jira.StatusRef) string {
	if ref == nil {
		return ""
	}
	return strings.TrimSpace(ref.Name)
}

func accountRefValue(ref *jira.AccountRef) string {
	if ref == nil {
		return ""
	}
	if value := strings.TrimSpace(ref.DisplayName); value != "" {
		return value
	}
	return strings.TrimSpace(ref.AccountID)
}

func asConverterError(err error) *converter.Error {
	var typed *converter.Error
	if errors.As(err, &typed) {
		return typed
	}
	return nil
}

func formatIssueError(code string, err error) string {
	if err == nil {
		return strings.TrimSpace(code)
	}
	if strings.TrimSpace(code) == "" {
		return strings.TrimSpace(err.Error())
	}
	return strings.TrimSpace("code=" + code + " " + err.Error())
}
