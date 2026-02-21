package httpclient

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/pat/jira-issue-sync/internal/contracts"
)

type Doer interface {
	Do(req *http.Request) (*http.Response, error)
}

type Options struct {
	Timeout      time.Duration
	MaxAttempts  int
	BaseBackoff  time.Duration
	RetryOnCodes map[int]struct{}
}

type Sleeper interface {
	Sleep(d time.Duration)
}

type RetryClient struct {
	doer        Doer
	timeout     time.Duration
	maxAttempts int
	baseBackoff time.Duration
	retryCodes  map[int]struct{}
	sleeper     Sleeper
}

func NewRetryClient(doer Doer, options Options) *RetryClient {
	resolved := resolveOptions(options)
	if doer == nil {
		doer = &http.Client{Timeout: resolved.Timeout}
	}

	return &RetryClient{
		doer:        doer,
		timeout:     resolved.Timeout,
		maxAttempts: resolved.MaxAttempts,
		baseBackoff: resolved.BaseBackoff,
		retryCodes:  resolved.RetryOnCodes,
		sleeper:     timeSleeper{},
	}
}

func (c *RetryClient) WithSleeper(sleeper Sleeper) *RetryClient {
	if c == nil {
		return nil
	}
	if sleeper == nil {
		return c
	}

	clone := *c
	clone.sleeper = sleeper
	return &clone
}

func (c *RetryClient) Do(req *http.Request) (*http.Response, error) {
	if c == nil {
		return nil, errors.New("retry client is nil")
	}
	if req == nil {
		return nil, errors.New("request is nil")
	}

	body, err := snapshotBody(req.Body)
	if err != nil {
		return nil, err
	}

	for attempt := 1; attempt <= c.maxAttempts; attempt++ {
		attemptReq := cloneRequest(req, body)
		attemptReq, cancel := withRequestTimeout(attemptReq, c.timeout)

		resp, err := c.doer.Do(attemptReq)
		if err != nil {
			cancel()
			if !shouldRetryError(err) || attempt == c.maxAttempts {
				return nil, err
			}
			c.sleep(backoffForAttempt(c.baseBackoff, attempt))
			continue
		}

		if !c.shouldRetryStatus(resp.StatusCode) || attempt == c.maxAttempts {
			if resp.Body != nil {
				resp.Body = &cancelOnCloseReadCloser{ReadCloser: resp.Body, cancel: cancel}
			} else {
				cancel()
			}
			return resp, nil
		}

		backoff := backoffForAttempt(c.baseBackoff, attempt)
		if retryAfter := parseRetryAfter(resp.Header.Get("Retry-After")); retryAfter > backoff {
			backoff = retryAfter
		}

		drainAndClose(resp.Body)
		cancel()
		c.sleep(backoff)
	}

	return nil, errors.New("request retries exhausted")
}

func (c *RetryClient) sleep(duration time.Duration) {
	if duration <= 0 {
		return
	}
	if c.sleeper == nil {
		return
	}
	c.sleeper.Sleep(duration)
}

func (c *RetryClient) shouldRetryStatus(statusCode int) bool {
	_, ok := c.retryCodes[statusCode]
	return ok
}

func resolveOptions(options Options) Options {
	resolved := options
	if resolved.Timeout <= 0 {
		resolved.Timeout = contracts.DefaultHTTPTimeout
	}
	if resolved.MaxAttempts <= 0 {
		resolved.MaxAttempts = contracts.DefaultRetryMaxAttempts
	}
	if resolved.BaseBackoff <= 0 {
		resolved.BaseBackoff = contracts.DefaultRetryBaseBackoff
	}
	if len(resolved.RetryOnCodes) == 0 {
		resolved.RetryOnCodes = map[int]struct{}{
			http.StatusTooManyRequests:     {},
			http.StatusInternalServerError: {},
			http.StatusBadGateway:          {},
			http.StatusServiceUnavailable:  {},
			http.StatusGatewayTimeout:      {},
		}
	}
	return resolved
}

func snapshotBody(body io.ReadCloser) ([]byte, error) {
	if body == nil {
		return nil, nil
	}
	defer body.Close()
	return io.ReadAll(body)
}

func cloneRequest(req *http.Request, body []byte) *http.Request {
	clone := req.Clone(req.Context())
	if body == nil {
		clone.Body = nil
		clone.GetBody = nil
		clone.ContentLength = 0
		return clone
	}

	clone.Body = io.NopCloser(bytes.NewReader(body))
	clone.ContentLength = int64(len(body))
	clone.GetBody = func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(body)), nil
	}
	return clone
}

func withRequestTimeout(req *http.Request, timeout time.Duration) (*http.Request, context.CancelFunc) {
	if timeout <= 0 {
		return req, func() {}
	}

	ctx, cancel := context.WithTimeout(req.Context(), timeout)
	return req.Clone(ctx), cancel
}

func shouldRetryError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}

	var netErr net.Error
	if errors.As(err, &netErr) {
		return netErr.Timeout() || netErr.Temporary()
	}
	return false
}

func backoffForAttempt(base time.Duration, attempt int) time.Duration {
	if base <= 0 || attempt <= 0 {
		return 0
	}
	factor := 1 << (attempt - 1)
	return time.Duration(factor) * base
}

func parseRetryAfter(value string) time.Duration {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return 0
	}

	if seconds, err := strconv.Atoi(trimmed); err == nil && seconds > 0 {
		return time.Duration(seconds) * time.Second
	}

	if when, err := http.ParseTime(trimmed); err == nil {
		delta := time.Until(when)
		if delta > 0 {
			return delta
		}
	}

	return 0
}

func drainAndClose(body io.ReadCloser) {
	if body == nil {
		return
	}
	_, _ = io.Copy(io.Discard, body)
	_ = body.Close()
}

type timeSleeper struct{}

func (timeSleeper) Sleep(d time.Duration) {
	time.Sleep(d)
}

type cancelOnCloseReadCloser struct {
	io.ReadCloser
	cancel context.CancelFunc
}

func (c *cancelOnCloseReadCloser) Close() error {
	if c == nil {
		return nil
	}
	if c.cancel != nil {
		c.cancel()
	}
	if c.ReadCloser == nil {
		return nil
	}
	return c.ReadCloser.Close()
}
