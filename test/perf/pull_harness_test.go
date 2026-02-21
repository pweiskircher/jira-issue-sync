package perf

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/pat/jira-issue-sync/internal/config"
	"github.com/pat/jira-issue-sync/internal/contracts"
	"github.com/pat/jira-issue-sync/internal/converter"
	httpclient "github.com/pat/jira-issue-sync/internal/http"
	"github.com/pat/jira-issue-sync/internal/jira"
	"github.com/pat/jira-issue-sync/internal/store"
	pullsync "github.com/pat/jira-issue-sync/internal/sync/pull"
)

const (
	perfTargetIssueCount = 200
	prdPullEnvelope      = 60 * time.Second
)

type pullHarnessConfig struct {
	IssueCount   int
	PageSize     int
	Concurrency  int
	HTTPTimeout  time.Duration
	MaxAttempts  int
	BaseBackoff  time.Duration
	RetryOnCodes map[int]struct{}
}

type pullGuardrails struct {
	MinPageSize    int
	MaxPageSize    int
	MinConcurrency int
	MaxConcurrency int
	MinTimeout     time.Duration
	MaxTimeout     time.Duration
	MinAttempts    int
	MaxAttempts    int
	MinBackoff     time.Duration
	MaxBackoff     time.Duration
}

var defaultPullGuardrails = pullGuardrails{
	MinPageSize:    25,
	MaxPageSize:    200,
	MinConcurrency: 1,
	MaxConcurrency: 16,
	MinTimeout:     5 * time.Second,
	MaxTimeout:     2 * time.Minute,
	MinAttempts:    1,
	MaxAttempts:    6,
	MinBackoff:     10 * time.Millisecond,
	MaxBackoff:     5 * time.Second,
}

type harnessMetrics struct {
	Duration             time.Duration
	PageRequests         int
	TotalSearchAttempts  int
	RetryAttempts        int
	PreparedMaxInFlight  int64
	PreparedIssueCount   int
	PersistedIssueCount  int
	SucceededIssueCount  int
	PRDEnvelopeSatisfied bool
}

func TestPullPerformanceHarness200Issues(t *testing.T) {
	cfg := pullHarnessConfig{
		IssueCount:   perfTargetIssueCount,
		PageSize:     40,
		Concurrency:  8,
		HTTPTimeout:  10 * time.Second,
		MaxAttempts:  3,
		BaseBackoff:  10 * time.Millisecond,
		RetryOnCodes: map[int]struct{}{http.StatusTooManyRequests: {}},
	}

	if err := validatePullHarnessConfig(cfg, defaultPullGuardrails); err != nil {
		t.Fatalf("expected config to satisfy guardrails, got %v", err)
	}

	metrics := runPullHarness(t, cfg)

	expectedPages := cfg.IssueCount / cfg.PageSize
	if cfg.IssueCount%cfg.PageSize != 0 {
		expectedPages++
	}

	if metrics.PageRequests != expectedPages {
		t.Fatalf("expected %d page requests, got %d", expectedPages, metrics.PageRequests)
	}

	if metrics.RetryAttempts != 1 {
		t.Fatalf("expected one retry attempt from injected 429, got %d", metrics.RetryAttempts)
	}

	if metrics.PreparedIssueCount != cfg.IssueCount || metrics.PersistedIssueCount != cfg.IssueCount || metrics.SucceededIssueCount != cfg.IssueCount {
		t.Fatalf("expected all issues to be prepared/persisted/successful, got prepared=%d persisted=%d succeeded=%d", metrics.PreparedIssueCount, metrics.PersistedIssueCount, metrics.SucceededIssueCount)
	}

	if metrics.PreparedMaxInFlight > int64(cfg.Concurrency) {
		t.Fatalf("expected max in-flight conversions <= concurrency %d, got %d", cfg.Concurrency, metrics.PreparedMaxInFlight)
	}

	if !metrics.PRDEnvelopeSatisfied {
		t.Fatalf("expected duration %s to satisfy PRD pull envelope %s", metrics.Duration, prdPullEnvelope)
	}

	t.Logf("pull harness metrics: duration=%s pages=%d attempts=%d retries=%d max_in_flight=%d", metrics.Duration, metrics.PageRequests, metrics.TotalSearchAttempts, metrics.RetryAttempts, metrics.PreparedMaxInFlight)
}

func TestPullHarnessTimeoutAndRetryBudget(t *testing.T) {
	cfg := pullHarnessConfig{
		IssueCount:  1,
		PageSize:    1,
		Concurrency: 1,
		HTTPTimeout: 25 * time.Millisecond,
		MaxAttempts: 3,
		BaseBackoff: 5 * time.Millisecond,
	}

	attempts := 0
	adapter, err := jira.NewCloudAdapter(jira.CloudAdapterOptions{
		BaseURL:  "https://example.atlassian.net",
		Email:    "perf@example.com",
		APIToken: "token",
		HTTPDoer: doerFunc(func(req *http.Request) (*http.Response, error) {
			attempts++
			<-req.Context().Done()
			return nil, req.Context().Err()
		}),
		RetryOptions: httpclient.Options{
			Timeout:     cfg.HTTPTimeout,
			MaxAttempts: cfg.MaxAttempts,
			BaseBackoff: cfg.BaseBackoff,
		},
	})
	if err != nil {
		t.Fatalf("failed to construct adapter: %v", err)
	}

	workspace := t.TempDir()
	writePullHarnessConfig(t, workspace)
	issueStore, err := store.New(filepath.Join(workspace, contracts.DefaultIssuesRootDir))
	if err != nil {
		t.Fatalf("failed to initialize issue store: %v", err)
	}

	pipeline := pullsync.Pipeline{
		Adapter:     adapter,
		Store:       issueStore,
		Converter:   &trackingConverter{},
		PageSize:    cfg.PageSize,
		Concurrency: cfg.Concurrency,
	}

	started := time.Now()
	_, err = pipeline.Execute(context.Background(), "project = PERF")
	elapsed := time.Since(started)
	if err == nil {
		t.Fatalf("expected timeout failure")
	}

	if attempts != cfg.MaxAttempts {
		t.Fatalf("expected %d attempts before failure, got %d", cfg.MaxAttempts, attempts)
	}

	minExpected := (time.Duration(cfg.MaxAttempts) * cfg.HTTPTimeout) + (cfg.BaseBackoff + 2*cfg.BaseBackoff)
	if elapsed < minExpected {
		t.Fatalf("expected elapsed time >= retry budget floor %s, got %s", minExpected, elapsed)
	}
}

func TestPullHarnessGuardrailsRejectOutOfEnvelopeTuning(t *testing.T) {
	tests := []struct {
		name string
		cfg  pullHarnessConfig
	}{
		{name: "page size too small", cfg: pullHarnessConfig{IssueCount: perfTargetIssueCount, PageSize: 10, Concurrency: 4, HTTPTimeout: 10 * time.Second, MaxAttempts: 3, BaseBackoff: 500 * time.Millisecond}},
		{name: "concurrency too high", cfg: pullHarnessConfig{IssueCount: perfTargetIssueCount, PageSize: 100, Concurrency: 64, HTTPTimeout: 10 * time.Second, MaxAttempts: 3, BaseBackoff: 500 * time.Millisecond}},
		{name: "timeout too low", cfg: pullHarnessConfig{IssueCount: perfTargetIssueCount, PageSize: 100, Concurrency: 4, HTTPTimeout: 2 * time.Second, MaxAttempts: 3, BaseBackoff: 500 * time.Millisecond}},
		{name: "retry attempts too high", cfg: pullHarnessConfig{IssueCount: perfTargetIssueCount, PageSize: 100, Concurrency: 4, HTTPTimeout: 10 * time.Second, MaxAttempts: 12, BaseBackoff: 500 * time.Millisecond}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if err := validatePullHarnessConfig(tc.cfg, defaultPullGuardrails); err == nil {
				t.Fatalf("expected guardrail validation failure")
			}
		})
	}
}

func runPullHarness(t *testing.T, cfg pullHarnessConfig) harnessMetrics {
	t.Helper()

	workspace := t.TempDir()
	writePullHarnessConfig(t, workspace)

	var attempts int32
	var retryAttempts int32
	pageSeen := map[int]bool{}
	pageMu := sync.Mutex{}

	adapter, err := jira.NewCloudAdapter(jira.CloudAdapterOptions{
		BaseURL:  "https://example.atlassian.net",
		Email:    "perf@example.com",
		APIToken: "token",
		HTTPDoer: doerFunc(func(req *http.Request) (*http.Response, error) {
			body, err := io.ReadAll(req.Body)
			if err != nil {
				return nil, err
			}
			_ = req.Body.Close()

			var payload struct {
				StartAt    int `json:"startAt"`
				MaxResults int `json:"maxResults"`
			}
			if err := json.Unmarshal(body, &payload); err != nil {
				return nil, err
			}

			atomic.AddInt32(&attempts, 1)

			pageMu.Lock()
			isFirstAttemptForPage := !pageSeen[payload.StartAt]
			if isFirstAttemptForPage {
				pageSeen[payload.StartAt] = true
			}
			pageMu.Unlock()

			if payload.StartAt == 0 && isFirstAttemptForPage {
				atomic.AddInt32(&retryAttempts, 1)
				return responseWithStatus(http.StatusTooManyRequests, `{"errorMessages":["rate limited"]}`), nil
			}

			issues := make([]map[string]any, 0, payload.MaxResults)
			for index := payload.StartAt; index < payload.StartAt+payload.MaxResults && index < cfg.IssueCount; index++ {
				issues = append(issues, map[string]any{
					"id":  fmt.Sprintf("%d", 1000+index),
					"key": fmt.Sprintf("PERF-%d", index+1),
					"fields": map[string]any{
						"summary": fmt.Sprintf("Synthetic issue %d", index+1),
						"description": map[string]any{
							"version": 1,
							"type":    "doc",
							"content": []map[string]any{{
								"type": "paragraph",
								"content": []map[string]any{{
									"type": "text",
									"text": fmt.Sprintf("Synthetic body %d", index+1),
								}},
							}},
						},
						"status":    map[string]any{"name": "Open"},
						"issuetype": map[string]any{"name": "Task"},
						"created":   "2026-02-20T00:00:00Z",
						"updated":   "2026-02-20T00:00:00Z",
					},
				})
			}

			responsePayload, err := json.Marshal(map[string]any{
				"startAt":    payload.StartAt,
				"maxResults": payload.MaxResults,
				"total":      cfg.IssueCount,
				"issues":     issues,
			})
			if err != nil {
				return nil, err
			}

			return responseWithStatus(http.StatusOK, string(responsePayload)), nil
		}),
		RetryOptions: httpclient.Options{
			Timeout:      cfg.HTTPTimeout,
			MaxAttempts:  cfg.MaxAttempts,
			BaseBackoff:  cfg.BaseBackoff,
			RetryOnCodes: cfg.RetryOnCodes,
		},
	})
	if err != nil {
		t.Fatalf("failed to construct cloud adapter: %v", err)
	}

	issueStore, err := store.New(filepath.Join(workspace, contracts.DefaultIssuesRootDir))
	if err != nil {
		t.Fatalf("failed to initialize issue store: %v", err)
	}

	converter := &trackingConverter{delay: 2 * time.Millisecond}
	pipeline := pullsync.Pipeline{
		Adapter:     adapter,
		Store:       issueStore,
		Converter:   converter,
		PageSize:    cfg.PageSize,
		Concurrency: cfg.Concurrency,
	}

	started := time.Now()
	result, err := pipeline.Execute(context.Background(), "project = PERF")
	if err != nil {
		t.Fatalf("expected pull harness success, got %v", err)
	}
	duration := time.Since(started)

	persisted := 0
	updated := 0
	for _, outcome := range result.Outcomes {
		if outcome.Updated {
			updated++
		}
	}
	persisted = len(result.Cache.Issues)

	metrics := harnessMetrics{
		Duration:             duration,
		PageRequests:         len(pageSeen),
		TotalSearchAttempts:  int(atomic.LoadInt32(&attempts)),
		RetryAttempts:        int(atomic.LoadInt32(&retryAttempts)),
		PreparedMaxInFlight:  atomic.LoadInt64(&converter.maxInFlight),
		PreparedIssueCount:   converter.markdownCalls(),
		PersistedIssueCount:  persisted,
		SucceededIssueCount:  updated,
		PRDEnvelopeSatisfied: duration <= prdPullEnvelope,
	}

	return metrics
}

func validatePullHarnessConfig(cfg pullHarnessConfig, guardrails pullGuardrails) error {
	if cfg.IssueCount < perfTargetIssueCount {
		return fmt.Errorf("issue count must be at least %d", perfTargetIssueCount)
	}
	if cfg.PageSize < guardrails.MinPageSize || cfg.PageSize > guardrails.MaxPageSize {
		return fmt.Errorf("page size %d out of guardrail range [%d,%d]", cfg.PageSize, guardrails.MinPageSize, guardrails.MaxPageSize)
	}
	if cfg.Concurrency < guardrails.MinConcurrency || cfg.Concurrency > guardrails.MaxConcurrency {
		return fmt.Errorf("concurrency %d out of guardrail range [%d,%d]", cfg.Concurrency, guardrails.MinConcurrency, guardrails.MaxConcurrency)
	}
	if cfg.HTTPTimeout < guardrails.MinTimeout || cfg.HTTPTimeout > guardrails.MaxTimeout {
		return fmt.Errorf("http timeout %s out of guardrail range [%s,%s]", cfg.HTTPTimeout, guardrails.MinTimeout, guardrails.MaxTimeout)
	}
	if cfg.MaxAttempts < guardrails.MinAttempts || cfg.MaxAttempts > guardrails.MaxAttempts {
		return fmt.Errorf("max attempts %d out of guardrail range [%d,%d]", cfg.MaxAttempts, guardrails.MinAttempts, guardrails.MaxAttempts)
	}
	if cfg.BaseBackoff < guardrails.MinBackoff || cfg.BaseBackoff > guardrails.MaxBackoff {
		return fmt.Errorf("base backoff %s out of guardrail range [%s,%s]", cfg.BaseBackoff, guardrails.MinBackoff, guardrails.MaxBackoff)
	}
	return nil
}

func writePullHarnessConfig(t *testing.T, workspace string) {
	t.Helper()

	cfg := contracts.Config{
		ConfigVersion: contracts.ConfigSchemaVersionV1,
		Profiles: map[string]contracts.ProjectProfile{
			"default": {
				ProjectKey: "PERF",
				DefaultJQL: "project = PERF",
			},
		},
	}

	if err := config.Write(filepath.Join(workspace, contracts.DefaultConfigFilePath), cfg); err != nil {
		t.Fatalf("failed to write pull harness config: %v", err)
	}
}

type trackingConverter struct {
	delay       time.Duration
	inFlight    int64
	maxInFlight int64
	calls       int64
}

func (c *trackingConverter) ToMarkdown(_ string) (converter.MarkdownResult, error) {
	current := atomic.AddInt64(&c.inFlight, 1)
	defer atomic.AddInt64(&c.inFlight, -1)
	atomic.AddInt64(&c.calls, 1)

	for {
		max := atomic.LoadInt64(&c.maxInFlight)
		if current <= max {
			break
		}
		if atomic.CompareAndSwapInt64(&c.maxInFlight, max, current) {
			break
		}
	}

	if c.delay > 0 {
		time.Sleep(c.delay)
	}
	return converter.MarkdownResult{Markdown: "synthetic markdown"}, nil
}

func (c *trackingConverter) ToADF(markdown string) (converter.ADFResult, error) {
	return converter.ADFResult{ADFJSON: markdown}, nil
}

func (c *trackingConverter) markdownCalls() int {
	return int(atomic.LoadInt64(&c.calls))
}

type doerFunc func(req *http.Request) (*http.Response, error)

func (f doerFunc) Do(req *http.Request) (*http.Response, error) {
	return f(req)
}

func responseWithStatus(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     make(http.Header),
		Body:       io.NopCloser(bytes.NewBufferString(body)),
	}
}
