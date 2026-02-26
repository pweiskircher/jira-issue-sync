package pull

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/pweiskircher/jira-issue-sync/internal/contracts"
	"github.com/pweiskircher/jira-issue-sync/internal/converter"
	"github.com/pweiskircher/jira-issue-sync/internal/issue"
	"github.com/pweiskircher/jira-issue-sync/internal/jira"
	"github.com/pweiskircher/jira-issue-sync/internal/store"
)

var defaultPullFields = []string{"*navigable"}

type Pipeline struct {
	Adapter            jira.Adapter
	Store              *store.Store
	Converter          converter.Adapter
	PageSize           int
	Concurrency        int
	Now                func() time.Time
	CustomFieldAliases map[string]string
	PullFields         []string
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
	changed         bool
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

	fetchFields := p.PullFields
	if len(fetchFields) == 0 {
		fetchFields = defaultPullFields
	}

	fetched, err := fetchIssues(ctx, p.Adapter, trimmedJQL, pageSize, fetchFields)
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

	prepared := prepareIssues(fetched, concurrency, now().UTC(), p.Converter, p.CustomFieldAliases)
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

		action := "unchanged"
		message := "issue unchanged"
		if entry.changed {
			action = "pull"
			message = "synchronized issue snapshot"
		}

		outcomes = append(outcomes, Outcome{
			Key:     entry.key,
			Action:  action,
			Status:  contracts.PerIssueStatusSuccess,
			Updated: entry.changed,
			Messages: []contracts.IssueMessage{{
				Level: "info",
				Text:  message,
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

		desiredPath, desiredPathErr := issuePath(entry.state, entry.key, entry.summary)
		if desiredPathErr != nil {
			entry.err = desiredPathErr
			entry.reasonCode = contracts.ReasonCodeValidationFailed
			entry.errorCode = "build_issue_path_failed"
			continue
		}

		if persistedUnchanged, unchangedErr := p.isPersistedIssueUnchanged(cache, *entry, desiredPath); unchangedErr != nil {
			entry.err = unchangedErr
			entry.reasonCode = contracts.ReasonCodeValidationFailed
			entry.errorCode = "read_existing_issue_failed"
			continue
		} else if persistedUnchanged {
			entry.changed = false
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

func issuePath(state store.IssueState, key string, summary string) (string, error) {
	dir := ""
	switch state {
	case store.IssueStateOpen:
		dir = "open"
	case store.IssueStateClosed:
		dir = "closed"
	default:
		return "", fmt.Errorf("unsupported issue state %q", state)
	}

	filename, err := issue.BuildFilename(strings.TrimSpace(key), summary)
	if err != nil {
		return "", err
	}

	return filepath.Join(dir, filename), nil
}

func (p Pipeline) isPersistedIssueUnchanged(cache store.Cache, entry preparedIssue, desiredPath string) (bool, error) {
	previous, ok := cache.Issues[entry.key]
	if !ok {
		return false, nil
	}
	if previous.Path != desiredPath {
		return false, nil
	}
	if previous.Status != string(entry.state) {
		return false, nil
	}
	if previous.RemoteUpdatedAt != entry.remoteUpdatedAt {
		return false, nil
	}

	existingIssue, issueExists, issueReadErr := p.readIfExists(previous.Path)
	if issueReadErr != nil {
		return false, issueReadErr
	}
	if !issueExists || !isCanonicalTextEqual(existingIssue, entry.canonical) {
		return false, nil
	}

	snapshotPath := filepath.Join(".sync", "originals", entry.key+".md")
	existingSnapshot, snapshotExists, snapshotReadErr := p.readIfExists(snapshotPath)
	if snapshotReadErr != nil {
		return false, snapshotReadErr
	}
	if !snapshotExists || !isCanonicalTextEqual(existingSnapshot, entry.canonical) {
		return false, nil
	}

	cache.Issues[entry.key] = previous
	return true, nil
}

func (p Pipeline) readIfExists(path string) ([]byte, bool, error) {
	content, err := p.Store.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false, nil
		}
		return nil, false, err
	}
	return content, true, nil
}

func isCanonicalTextEqual(existing []byte, canonical string) bool {
	return bytes.Equal(normalizePullText(string(existing)), normalizePullText(canonical))
}

func normalizePullText(input string) []byte {
	normalized := contracts.NormalizeSingleValue(contracts.NormalizationNormalizeLineEndings, input)
	if normalized == "" {
		return []byte{}
	}

	lines := strings.Split(normalized, "\n")
	filtered := make([]string, 0, len(lines))
	for _, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "synced_at:") {
			continue
		}
		filtered = append(filtered, line)
	}

	normalized = strings.Join(filtered, "\n")
	if !strings.HasSuffix(normalized, "\n") {
		normalized += "\n"
	}
	return []byte(normalized)
}

func fetchIssues(ctx context.Context, adapter jira.Adapter, jql string, pageSize int, fields []string) ([]jira.Issue, error) {
	issues := make([]jira.Issue, 0)
	startAt := 0
	nextPageToken := ""
	usingTokenPagination := false

	for {
		response, err := adapter.SearchIssues(ctx, jira.SearchIssuesRequest{
			JQL:           jql,
			StartAt:       startAt,
			MaxResults:    pageSize,
			Fields:        append([]string(nil), fields...),
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

func prepareIssues(issues []jira.Issue, concurrency int, syncedAt time.Time, markdownConverter converter.Adapter, customFieldAliases map[string]string) []preparedIssue {
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
				prepared[index] = prepareIssue(issues[index], syncedAt, markdownConverter, customFieldAliases)
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

func prepareIssue(remote jira.Issue, syncedAt time.Time, markdownConverter converter.Adapter, customFieldAliases map[string]string) preparedIssue {
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
			CustomFields:  mapAliasedCustomFields(remote.Fields.CustomFields, customFieldAliases),
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
		changed:         true,
	}
}

func issueStateFromStatus(status string) store.IssueState {
	normalized := strings.ToLower(strings.TrimSpace(status))
	switch normalized {
	case "done", "closed", "resolved", "complete", "completed", "rejected", "declined", "cancelled", "canceled", "won't do", "wont do":
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

func mapAliasedCustomFields(values map[string]json.RawMessage, aliases map[string]string) map[string]json.RawMessage {
	if len(values) == 0 || len(aliases) == 0 {
		return nil
	}
	keys := make([]string, 0, len(aliases))
	for key := range aliases {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	mapped := make(map[string]json.RawMessage)
	for _, fieldID := range keys {
		alias := strings.TrimSpace(aliases[fieldID])
		if alias == "" {
			continue
		}
		value, ok := values[fieldID]
		if !ok {
			continue
		}
		if _, exists := mapped[alias]; exists {
			continue
		}
		mapped[alias] = append(json.RawMessage(nil), value...)
	}
	if len(mapped) == 0 {
		return nil
	}
	return mapped
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
