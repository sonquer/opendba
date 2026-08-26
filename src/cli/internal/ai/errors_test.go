package ai

import (
	"errors"
	"fmt"
	"net/http"
	"testing"
)

func TestErrorMessage(t *testing.T) {
	cases := map[string]struct {
		failure *Error
		want    string
	}{
		"instance and message": {
			failure: &Error{Reason: ReasonAuth, Instance: "claude", Message: "invalid key"},
			want:    "claude: auth: invalid key",
		},
		"no instance": {
			failure: &Error{Reason: ReasonQuota, Message: "out of credit"},
			want:    "quota: out of credit",
		},
		"falls back to the wrapped error": {
			failure: &Error{Reason: ReasonUnavailable, Err: errors.New("dial refused")},
			want:    "unavailable: dial refused",
		},
		"nothing to say": {
			failure: &Error{Reason: ReasonDecode},
			want:    "decode: no detail",
		},
	}
	for name, test := range cases {
		t.Run(name, func(t *testing.T) {
			if got := test.failure.Error(); got != test.want {
				t.Fatalf("Error() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestErrorUnwrap(t *testing.T) {
	inner := errors.New("broken pipe")
	failure := Failure(ReasonProvider, "local", "generate", inner)
	if !errors.Is(failure, inner) {
		t.Fatal("wrapped error is not reachable through errors.Is")
	}
	wrapped := fmt.Errorf("chat: %w", failure)
	var found *Error
	if !errors.As(wrapped, &found) || found.Reason != ReasonProvider {
		t.Fatalf("errors.As did not find the failure, got %v", found)
	}
}

func TestRetryable(t *testing.T) {
	cases := map[string]struct {
		reason Reason
		want   bool
	}{
		"rate limit":       {reason: ReasonRateLimit, want: true},
		"provider":         {reason: ReasonProvider, want: true},
		"unavailable":      {reason: ReasonUnavailable, want: true},
		"auth":             {reason: ReasonAuth, want: false},
		"quota":            {reason: ReasonQuota, want: false},
		"context overflow": {reason: ReasonContextOverflow, want: false},
		"content policy":   {reason: ReasonContentPolicy, want: false},
		"request":          {reason: ReasonRequest, want: false},
		"decode":           {reason: ReasonDecode, want: false},
	}
	for name, test := range cases {
		t.Run(name, func(t *testing.T) {
			failure := &Error{Reason: test.reason}
			if got := failure.Retryable(); got != test.want {
				t.Fatalf("Retryable() = %v, want %v", got, test.want)
			}
			if got := Retryable(fmt.Errorf("wrapped: %w", failure)); got != test.want {
				t.Fatalf("Retryable(err) = %v, want %v", got, test.want)
			}
		})
	}
}

func TestRetryableUnclassified(t *testing.T) {
	if Retryable(errors.New("who knows")) {
		t.Fatal("an unclassified error must not be retried")
	}
	if _, ok := ReasonOf(errors.New("who knows")); ok {
		t.Fatal("an unclassified error must not report a reason")
	}
	reason, ok := ReasonOf(Failure(ReasonAuth, "claude", "no key", nil))
	if !ok || reason != ReasonAuth {
		t.Fatalf("ReasonOf() = %q, %v, want auth, true", reason, ok)
	}
}

func TestClassify(t *testing.T) {
	cases := map[string]struct {
		status int
		body   string
		want   Reason
	}{
		"unauthorised":              {status: http.StatusUnauthorized, want: ReasonAuth},
		"forbidden":                 {status: http.StatusForbidden, want: ReasonAuth},
		"too many requests":         {status: http.StatusTooManyRequests, want: ReasonRateLimit},
		"payment required":          {status: http.StatusPaymentRequired, want: ReasonQuota},
		"entity too large":          {status: http.StatusRequestEntityTooLarge, want: ReasonContextOverflow},
		"server error":              {status: http.StatusInternalServerError, want: ReasonProvider},
		"bad gateway":               {status: http.StatusBadGateway, want: ReasonProvider},
		"long prompt is not a bug":  {status: http.StatusBadRequest, body: "This model's maximum context length is 8192 tokens", want: ReasonContextOverflow},
		"filtered":                  {status: http.StatusBadRequest, body: "blocked by the content policy", want: ReasonContentPolicy},
		"plain bad request":         {status: http.StatusBadRequest, body: "unknown field temperature2", want: ReasonRequest},
		"not found":                 {status: http.StatusNotFound, body: "no such model", want: ReasonRequest},
		"success is never expected": {status: http.StatusOK, body: "", want: ReasonProvider},
	}
	for name, test := range cases {
		t.Run(name, func(t *testing.T) {
			if got := Classify(test.status, test.body); got != test.want {
				t.Fatalf("Classify(%d, %q) = %q, want %q", test.status, test.body, got, test.want)
			}
		})
	}
}

func TestIsContextOverflow(t *testing.T) {
	cases := map[string]struct {
		message string
		want    bool
	}{
		"maximum context":        {message: "maximum context length exceeded", want: true},
		"context window":         {message: "Requested tokens exceed context window of 4096", want: true},
		"prompt too long":        {message: "prompt is too long: 210000 tokens", want: true},
		"asked to shorten":       {message: "Please reduce the length of the messages", want: true},
		"input too long":         {message: "input is too long for this model", want: true},
		"too many tokens":        {message: "too many tokens in request", want: true},
		"exceeds the maximum":    {message: "the request exceeds the maximum allowed", want: true},
		"rate limit is excluded": {message: "rate limit reached: too many tokens per minute", want: false},
		"throttling is excluded": {message: "too many requests, context window busy", want: false},
		"outage is excluded":     {message: "service unavailable, maximum context nodes down", want: false},
		"unrelated":              {message: "invalid api key", want: false},
		"empty":                  {message: "", want: false},
	}
	for name, test := range cases {
		t.Run(name, func(t *testing.T) {
			if got := IsContextOverflow(test.message); got != test.want {
				t.Fatalf("IsContextOverflow(%q) = %v, want %v", test.message, got, test.want)
			}
		})
	}
}
