// Package httpx provides the shared HTTP client for internal
// service-to-service calls: a total timeout, bounded retries with jitter
// for idempotent requests, X-Internal-Token injection, and an optional
// base transport so callers can hook in tracing/metrics later.
package httpx

import (
	"io"
	"math/rand/v2"
	"net/http"
	"time"
)

const (
	// DefaultTimeout is the total request budget, retries included,
	// applied when Options.Timeout is zero.
	DefaultTimeout = 10 * time.Second

	// InternalTokenHeader carries the shared secret between internal services.
	InternalTokenHeader = "X-Internal-Token"

	retryBaseDelay = 200 * time.Millisecond
	retryMaxDelay  = 2 * time.Second
)

// Options configures the client returned by New.
type Options struct {
	// Timeout is the total budget including any retries. It is enforced
	// by http.Client around the whole exchange. Zero means DefaultTimeout.
	Timeout time.Duration

	// MaxRetries is the number of extra attempts made for idempotent
	// requests (GET, HEAD, OPTIONS, TRACE, PUT, DELETE) that fail with a
	// network error or a 429/5xx response. Zero disables retries; there
	// is no default.
	MaxRetries int

	// InternalToken is sent as X-Internal-Token on every request when
	// non-empty. Requests that already carry the header are left alone.
	InternalToken string

	// Base is the underlying transport. nil means http.DefaultTransport.
	// Wrap it to add tracing/metrics without losing the behaviour here.
	Base http.RoundTripper

	// Log, when set, is called once per retry.
	Log func(format string, args ...any)
}

// New builds an *http.Client wired with the retry and internal-token
// transports. The returned client is safe for concurrent use and intended
// to be shared per upstream service.
func New(opt Options) *http.Client {
	timeout := opt.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}

	base := opt.Base
	if base == nil {
		base = http.DefaultTransport
	}

	var rt http.RoundTripper = base
	if opt.MaxRetries > 0 {
		rt = &retryTransport{base: rt, maxRetries: opt.MaxRetries, log: opt.Log}
	}
	if opt.InternalToken != "" {
		rt = &tokenTransport{base: rt, token: opt.InternalToken}
	}

	return &http.Client{Transport: rt, Timeout: timeout}
}

// tokenTransport stamps the shared internal secret on every request.
type tokenTransport struct {
	base  http.RoundTripper
	token string
}

func (t *tokenTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.Header.Get(InternalTokenHeader) == "" {
		req.Header.Set(InternalTokenHeader, t.token)
	}
	return t.base.RoundTrip(req)
}

// retryTransport retries idempotent requests on network errors and
// 429/5xx responses, with exponential backoff and jitter.
type retryTransport struct {
	base       http.RoundTripper
	maxRetries int
	log        func(format string, args ...any)
}

func (t *retryTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// Only retry when the body can be re-sent: no body, or one GetBody can recreate.
	replayable := req.Body == nil || req.GetBody != nil
	idempotent := req.Method == http.MethodGet ||
		req.Method == http.MethodHead ||
		req.Method == http.MethodOptions ||
		req.Method == http.MethodTrace ||
		req.Method == http.MethodPut ||
		req.Method == http.MethodDelete
	if !replayable || !idempotent {
		return t.base.RoundTrip(req)
	}

	var resp *http.Response
	var err error
	for attempt := 0; ; attempt++ {
		if req.GetBody != nil {
			body, bodyErr := req.GetBody()
			if bodyErr != nil {
				return nil, bodyErr
			}
			req.Body = body
		}

		resp, err = t.base.RoundTrip(req)

		if attempt >= t.maxRetries || !shouldRetry(req.Context().Err(), resp, err) {
			return resp, err
		}

		if t.log != nil {
			status := 0
			if resp != nil {
				status = resp.StatusCode
			}
			t.log("httpx: retrying %s %s (attempt %d/%d, last error: %v, status: %d)",
				req.Method, req.URL, attempt+1, t.maxRetries, err, status)
		}

		// Free the connection before sleeping.
		if resp != nil {
			drain(resp)
		}

		select {
		case <-req.Context().Done():
			return nil, req.Context().Err()
		case <-time.After(backoff(attempt)):
		}
	}
}

// shouldRetry reports whether a failed exchange is worth retrying.
// Caller cancellation and 4xx responses (other than 429) are not.
func shouldRetry(ctxErr error, resp *http.Response, err error) bool {
	if ctxErr != nil {
		return false
	}
	if err != nil {
		return true // network / protocol error
	}
	if resp == nil {
		return true
	}
	return resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500
}

func drain(resp *http.Response) {
	if resp.Body != nil {
		io.Copy(io.Discard, io.LimitReader(resp.Body, 4<<10))
		resp.Body.Close()
	}
}

func backoff(attempt int) time.Duration {
	delay := retryBaseDelay << attempt
	if delay > retryMaxDelay || delay <= 0 {
		delay = retryMaxDelay
	}
	return delay + time.Duration(rand.Int64N(int64(retryBaseDelay)))
}

var (
	_ http.RoundTripper = (*tokenTransport)(nil)
	_ http.RoundTripper = (*retryTransport)(nil)
)
