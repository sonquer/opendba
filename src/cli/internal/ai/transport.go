package ai

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	maxErrorBody     = 8 << 10
	defaultAttempts  = 4
	defaultBackoff   = 500 * time.Millisecond
	maxRetryWaitTime = 60 * time.Second
)

// Transport sends the requests of one instance.
type Transport struct {
	HTTP     Doer
	Instance string
	Attempts int
	Backoff  time.Duration
	Sleep    func(ctx context.Context, d time.Duration) error
}

// NewTransport returns a transport with the default retry policy.
func NewTransport(client Doer, instance string) *Transport {
	return &Transport{
		HTTP:     client,
		Instance: instance,
		Attempts: defaultAttempts,
		Backoff:  defaultBackoff,
		Sleep:    wait,
	}
}

// Build makes one request. It is called again for every attempt, because a
// request body is a reader and a retry needs a fresh one.
type Build func() (*http.Request, error)

// Response is what a back-end reads.
type Response struct {
	Status int
	Header http.Header
	Body   io.ReadCloser
}

// Do sends a request, retrying the failures worth retrying, and returns a
// response whose body the caller closes.
func (t *Transport) Do(ctx context.Context, build Build) (*Response, error) {
	attempts := t.Attempts
	if attempts < 1 {
		attempts = 1
	}
	var last error
	for attempt := range attempts {
		if attempt > 0 {
			if err := t.pause(ctx, attempt, last); err != nil {
				return nil, err
			}
		}
		response, err := t.attempt(ctx, build)
		if err == nil {
			return &Response{Status: response.StatusCode, Header: response.Header, Body: response.Body}, nil
		}
		last = err
		if !Retryable(err) {
			return nil, err
		}
	}
	return nil, last
}

func (t *Transport) attempt(ctx context.Context, build Build) (*http.Response, error) {
	request, err := build()
	if err != nil {
		return nil, Failure(ReasonRequest, t.Instance, "build request", err)
	}
	response, err := t.HTTP.Do(request.WithContext(ctx))
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, Failure(ReasonUnavailable, t.Instance, "send request", err)
	}
	if response.StatusCode >= 200 && response.StatusCode < 300 {
		return response, nil
	}
	return nil, t.failure(response)
}

func (t *Transport) failure(response *http.Response) error {
	body, _ := io.ReadAll(io.LimitReader(response.Body, maxErrorBody))
	_ = response.Body.Close()
	message := strings.TrimSpace(string(body))
	failure := &Error{
		Reason:   Classify(response.StatusCode, message),
		Instance: t.Instance,
		Status:   response.StatusCode,
		Message:  summarise(message, response.StatusCode),
		RetryIn:  retryAfter(response.Header),
	}
	return failure
}

func (t *Transport) pause(ctx context.Context, attempt int, last error) error {
	sleep := t.Sleep
	if sleep == nil {
		sleep = wait
	}
	return sleep(ctx, t.delay(attempt, last))
}

func (t *Transport) delay(attempt int, last error) time.Duration {
	var failure *Error
	if errors.As(last, &failure) && failure.RetryIn > 0 {
		if failure.RetryIn > maxRetryWaitTime {
			return maxRetryWaitTime
		}
		return failure.RetryIn
	}
	base := t.Backoff
	if base <= 0 {
		base = defaultBackoff
	}
	return base << (attempt - 1)
}

func summarise(body string, status int) string {
	if body == "" {
		return fmt.Sprintf("http %d", status)
	}
	if len(body) > 400 {
		return body[:400]
	}
	return body
}

// retryAfter reads the wait a provider asked for, in either of the two forms
// the header is allowed to take.
func retryAfter(header http.Header) time.Duration {
	value := strings.TrimSpace(header.Get("Retry-After"))
	if value == "" {
		return 0
	}
	if seconds, err := strconv.Atoi(value); err == nil && seconds >= 0 {
		return time.Duration(seconds) * time.Second
	}
	if when, err := http.ParseTime(value); err == nil {
		if until := time.Until(when); until > 0 {
			return until
		}
	}
	return 0
}

func wait(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
