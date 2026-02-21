package publish

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/pat/jira-issue-sync/internal/contracts"
	"github.com/pat/jira-issue-sync/internal/converter"
	"github.com/pat/jira-issue-sync/internal/issue"
	"github.com/pat/jira-issue-sync/internal/jira"
	"github.com/pat/jira-issue-sync/internal/store"
)

type Options struct {
	Adapter    jira.Adapter
	Store      *store.Store
	Converter  converter.Adapter
	ProjectKey string
}

type Input struct {
	LocalKey     string
	RelativePath string
	Document     issue.Document
}

type Result struct {
	RemoteKey string
	Created   bool
}

func PublishDraft(ctx context.Context, options Options, input Input) (Result, error) {
	if options.Adapter == nil {
		return Result{}, fmt.Errorf("publish adapter is not configured")
	}
	if options.Store == nil {
		return Result{}, fmt.Errorf("publish store is not configured")
	}
	if options.Converter == nil {
		return Result{}, fmt.Errorf("publish converter is not configured")
	}

	localKey := strings.TrimSpace(input.LocalKey)
	if !contracts.LocalDraftKeyPattern.MatchString(localKey) {
		return Result{}, fmt.Errorf("draft publish requires local key in L-<hex> format")
	}

	projectKey := strings.TrimSpace(options.ProjectKey)
	if projectKey == "" {
		return Result{}, fmt.Errorf("draft publish requires project key")
	}

	remoteKey, err := loadPublishedKeyMarker(options.Store, localKey)
	if err != nil {
		return Result{}, err
	}

	created := false
	if remoteKey == "" {
		createRequest, requestErr := buildCreateIssueRequest(projectKey, input.Document, options.Converter)
		if requestErr != nil {
			return Result{}, requestErr
		}
		createdIssue, createErr := options.Adapter.CreateIssue(ctx, createRequest)
		if createErr != nil {
			return Result{}, createErr
		}
		remoteKey = strings.TrimSpace(createdIssue.Key)
		if !contracts.JiraIssueKeyPattern.MatchString(remoteKey) {
			return Result{}, fmt.Errorf("jira create issue response returned invalid key")
		}
		created = true
	}

	published, canonical, err := renderPublishedDocument(input.Document, localKey, remoteKey)
	if err != nil {
		return Result{}, err
	}

	if _, err := options.Store.WriteOriginalSnapshot(localKey, canonical); err != nil {
		return Result{}, err
	}
	if _, err := options.Store.WriteOriginalSnapshot(remoteKey, canonical); err != nil {
		return Result{}, err
	}

	targetFilename, err := issue.BuildFilename(remoteKey, published.FrontMatter.Summary)
	if err != nil {
		return Result{}, err
	}
	targetPath := filepath.Join(filepath.Dir(input.RelativePath), targetFilename)

	if err := options.Store.WriteFile(targetPath, []byte(canonical)); err != nil {
		return Result{}, err
	}
	if targetPath != input.RelativePath {
		if err := options.Store.Remove(input.RelativePath); err != nil {
			return Result{}, err
		}
	}

	if err := options.Store.Remove(localSnapshotPath(localKey)); err != nil {
		return Result{}, err
	}

	return Result{RemoteKey: remoteKey, Created: created}, nil
}

func buildCreateIssueRequest(projectKey string, local issue.Document, markdownConverter converter.Adapter) (jira.CreateIssueRequest, error) {
	request := jira.CreateIssueRequest{
		ProjectKey:        projectKey,
		IssueTypeName:     strings.TrimSpace(local.FrontMatter.IssueType),
		Summary:           strings.TrimSpace(local.FrontMatter.Summary),
		Labels:            append([]string(nil), local.FrontMatter.Labels...),
		AssigneeAccountID: strings.TrimSpace(local.FrontMatter.Assignee),
		PriorityName:      strings.TrimSpace(local.FrontMatter.Priority),
	}

	description := strings.TrimSpace(local.MarkdownBody)
	if description == "" {
		return request, nil
	}

	adfResult, err := markdownConverter.ToADF(description)
	if err != nil {
		return jira.CreateIssueRequest{}, fmt.Errorf("failed to convert markdown description to adf: %w", err)
	}
	trimmed := strings.TrimSpace(adfResult.ADFJSON)
	if trimmed == "" {
		return request, nil
	}
	request.Description = json.RawMessage(trimmed)
	return request, nil
}

func renderPublishedDocument(local issue.Document, localKey string, remoteKey string) (issue.Document, string, error) {
	rewritten := local
	rewritten.CanonicalKey = remoteKey
	rewritten.FrontMatter.Key = remoteKey
	rewritten.MarkdownBody = contracts.RewriteTempIDReferences(local.MarkdownBody, map[string]string{localKey: remoteKey})

	canonical, err := issue.RenderDocument(rewritten)
	if err != nil {
		return issue.Document{}, "", err
	}
	return rewritten, canonical, nil
}

func loadPublishedKeyMarker(workspaceStore *store.Store, localKey string) (string, error) {
	content, err := workspaceStore.ReadFile(localSnapshotPath(localKey))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", nil
		}
		return "", err
	}

	doc, parseErr := issue.ParseDocument(localSnapshotPath(localKey), string(content))
	if parseErr != nil {
		return "", parseErr
	}
	markerKey := strings.TrimSpace(doc.CanonicalKey)
	if markerKey == "" {
		markerKey = strings.TrimSpace(doc.FrontMatter.Key)
	}
	if markerKey == "" {
		return "", nil
	}
	if !contracts.JiraIssueKeyPattern.MatchString(markerKey) {
		return "", nil
	}
	return markerKey, nil
}

func localSnapshotPath(localKey string) string {
	return filepath.Join(".sync", "originals", localKey+".md")
}
