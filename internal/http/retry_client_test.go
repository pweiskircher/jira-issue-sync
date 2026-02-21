package httpclient

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/pat/jira-issue-sync/internal/contracts"
)

func TestRetryClientRetriesRetryableStatusesWithExponentialBackoff(t *testing.T) {
	t.Parallel()

	attempts := 0
	requestBodies := make([]string, 0)
	mu := sync.Mutex{}
	sleeper := &recordingSleeper{}

	client := NewRetryClient(doerFunc(func(req *http.Request) (*http.Response, error) {
		payload, err := io.ReadAll(req.Body)
		if err != nil {
			return nil, err
		}
		mu.Lock()
		requestBodies = append(requestBodies, string(payload))
		attempts++
		current := attempts
		mu.Unlock()

		if current < 3 {
			return responseWithStatus(http.StatusServiceUnavailable, "retry"), nil
		}
		return responseWithStatus(http.StatusOK, "ok"), nil
	}), Options{
		Timeout:     time.Second,
		MaxAttempts: 3,
		BaseBackoff: 25 * time.Millisecond,
	}).WithSleeper(sleeper)

	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, "https://example.test", strings.NewReader("payload"))
	if err != nil {
		t.Fatalf("expected request creation success, got %v", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("expected retries to succeed, got %v", err)
	}
	t.Cleanup(func() {
		_ = resp.Body.Close()
	})

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 status after retries, got %d", resp.StatusCode)
	}

	mu.Lock()
	defer mu.Unlock()
	if attempts != 3 {
		t.Fatalf("expected 3 attempts, got %d", attempts)
	}
	for i, payload := range requestBodies {
		if payload != "payload" {
			t.Fatalf("expected attempt %d payload to be replayed, got %q", i+1, payload)
		}
	}

	if len(sleeper.calls) != 2 {
		t.Fatalf("expected 2 backoff sleeps, got %d", len(sleeper.calls))
	}
	if sleeper.calls[0] != 25*time.Millisecond || sleeper.calls[1] != 50*time.Millisecond {
		t.Fatalf("unexpected backoff sequence: %#v", sleeper.calls)
	}
}

func TestRetryClientDoesNotRetryNonRetryableStatus(t *testing.T) {
	t.Parallel()

	attempts := 0
	sleeper := &recordingSleeper{}
	client := NewRetryClient(doerFunc(func(req *http.Request) (*http.Response, error) {
		attempts++
		return responseWithStatus(http.StatusBadRequest, "bad request"), nil
	}), Options{
		MaxAttempts: 3,
		BaseBackoff: 10 * time.Millisecond,
	}).WithSleeper(sleeper)

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "https://example.test", nil)
	if err != nil {
		t.Fatalf("expected request creation success, got %v", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("expected response return for non-retryable status, got %v", err)
	}
	t.Cleanup(func() {
		_ = resp.Body.Close()
	})

	if attempts != 1 {
		t.Fatalf("expected a single attempt, got %d", attempts)
	}
	if len(sleeper.calls) != 0 {
		t.Fatalf("expected no backoff for non-retryable status, got %#v", sleeper.calls)
	}
}

func TestRetryClientRetriesTransientErrors(t *testing.T) {
	t.Parallel()

	attempts := 0
	sleeper := &recordingSleeper{}
	client := NewRetryClient(doerFunc(func(req *http.Request) (*http.Response, error) {
		attempts++
		if attempts < 3 {
			return nil, context.DeadlineExceeded
		}
		return responseWithStatus(http.StatusOK, "ok"), nil
	}), Options{
		MaxAttempts: 3,
		BaseBackoff: 20 * time.Millisecond,
	}).WithSleeper(sleeper)

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "https://example.test", nil)
	if err != nil {
		t.Fatalf("expected request creation success, got %v", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("expected retries to recover from transient errors, got %v", err)
	}
	t.Cleanup(func() {
		_ = resp.Body.Close()
	})

	if attempts != 3 {
		t.Fatalf("expected 3 attempts, got %d", attempts)
	}
}

func TestRetryClientAppliesPerAttemptTimeout(t *testing.T) {
	t.Parallel()

	client := NewRetryClient(doerFunc(func(req *http.Request) (*http.Response, error) {
		<-req.Context().Done()
		return nil, req.Context().Err()
	}), Options{
		Timeout:     15 * time.Millisecond,
		MaxAttempts: 1,
	})

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "https://example.test", nil)
	if err != nil {
		t.Fatalf("expected request creation success, got %v", err)
	}

	start := time.Now()
	_, err = client.Do(req)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected deadline exceeded, got %v", err)
	}

	if elapsed := time.Since(start); elapsed > 250*time.Millisecond {
		t.Fatalf("expected timeout-bound attempt, took %s", elapsed)
	}
}

func TestRetryClientRespectsRetryAfterWhenLargerThanBaseBackoff(t *testing.T) {
	t.Parallel()

	attempts := 0
	sleeper := &recordingSleeper{}
	client := NewRetryClient(doerFunc(func(req *http.Request) (*http.Response, error) {
		attempts++
		if attempts == 1 {
			resp := responseWithStatus(http.StatusTooManyRequests, "rate limited")
			resp.Header.Set("Retry-After", "2")
			return resp, nil
		}
		return responseWithStatus(http.StatusOK, "ok"), nil
	}), Options{
		MaxAttempts: 2,
		BaseBackoff: 10 * time.Millisecond,
	}).WithSleeper(sleeper)

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "https://example.test", nil)
	if err != nil {
		t.Fatalf("expected request creation success, got %v", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("expected retry-after retry success, got %v", err)
	}
	t.Cleanup(func() {
		_ = resp.Body.Close()
	})

	if len(sleeper.calls) != 1 {
		t.Fatalf("expected one sleep from retry-after, got %#v", sleeper.calls)
	}
	if sleeper.calls[0] != 2*time.Second {
		t.Fatalf("expected retry-after sleep of 2s, got %s", sleeper.calls[0])
	}
}

func TestRetryClientUsesContractDefaultsWhenOptionsUnset(t *testing.T) {
	t.Parallel()

	client := NewRetryClient(nil, Options{})
	if client.timeout != contracts.DefaultHTTPTimeout {
		t.Fatalf("expected default timeout %s, got %s", contracts.DefaultHTTPTimeout, client.timeout)
	}
	if client.maxAttempts != contracts.DefaultRetryMaxAttempts {
		t.Fatalf("expected default max attempts %d, got %d", contracts.DefaultRetryMaxAttempts, client.maxAttempts)
	}
	if client.baseBackoff != contracts.DefaultRetryBaseBackoff {
		t.Fatalf("expected default base backoff %s, got %s", contracts.DefaultRetryBaseBackoff, client.baseBackoff)
	}
	if _, ok := client.retryCodes[http.StatusTooManyRequests]; !ok {
		t.Fatalf("expected default retry codes to include HTTP 429")
	}
}

type doerFunc func(req *http.Request) (*http.Response, error)

func (f doerFunc) Do(req *http.Request) (*http.Response, error) {
	return f(req)
}

type recordingSleeper struct {
	calls []time.Duration
}

func (s *recordingSleeper) Sleep(d time.Duration) {
	s.calls = append(s.calls, d)
}

func responseWithStatus(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     make(http.Header),
	}
}
