package ai

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// Reason classifies a failure. A caller decides what to do from this rather than
// from a message, because the message is a string a provider may change and the
// four providers do not agree on any of them.
type Reason string

const (
	ReasonAuth            Reason = "auth"
	ReasonRateLimit       Reason = "rate limit"
	ReasonQuota           Reason = "quota"
	ReasonContextOverflow Reason = "context overflow"
	ReasonContentPolicy   Reason = "content policy"
	ReasonProvider        Reason = "provider"
	ReasonRequest         Reason = "request"
	ReasonUnavailable     Reason = "unavailable"
	ReasonDecode          Reason = "decode"
)

// Error is a classified failure from a back-end.
type Error struct {
	Reason   Reason
	Instance string
	Status   int
	Message  string
	RetryIn  time.Duration
	Err      error
}

func (e *Error) Error() string {
	message := e.Message
	if message == "" && e.Err != nil {
		message = e.Err.Error()
	}
	if message == "" {
		message = "no detail"
	}
	if e.Instance == "" {
		return fmt.Sprintf("%s: %s", e.Reason, message)
	}
	return fmt.Sprintf("%s: %s: %s", e.Instance, e.Reason, message)
}

func (e *Error) Unwrap() error { return e.Err }

// Retryable reports whether the same request could succeed if it were sent
// again.
func (e *Error) Retryable() bool {
	switch e.Reason {
	case ReasonRateLimit, ReasonProvider, ReasonUnavailable:
		return true
	default:
		return false
	}
}

// Failure builds a classified error, keeping the underlying error reachable
// through errors.Is and errors.As.
func Failure(reason Reason, instance, message string, err error) *Error {
	return &Error{Reason: reason, Instance: instance, Message: message, Err: err}
}

// ReasonOf reports the classification of an error, and whether it had one.
func ReasonOf(err error) (Reason, bool) {
	var failure *Error
	if errors.As(err, &failure) {
		return failure.Reason, true
	}
	return "", false
}

// Retryable reports whether an error is worth trying again. An unclassified
// error is not, because nothing is known about it.
func Retryable(err error) bool {
	var failure *Error
	if errors.As(err, &failure) {
		return failure.Retryable()
	}
	return false
}

// Classify turns an HTTP status and whatever the body said into a reason.
func Classify(status int, body string) Reason {
	switch {
	case status == http.StatusUnauthorized, status == http.StatusForbidden:
		return ReasonAuth
	case status == http.StatusTooManyRequests:
		return ReasonRateLimit
	case status == http.StatusPaymentRequired:
		return ReasonQuota
	case status == http.StatusRequestEntityTooLarge:
		return ReasonContextOverflow
	case status >= 500:
		return ReasonProvider
	}
	if IsContextOverflow(body) {
		return ReasonContextOverflow
	}
	if isContentPolicy(body) {
		return ReasonContentPolicy
	}
	if status >= 400 {
		return ReasonRequest
	}
	return ReasonProvider
}

var overflowPhrases = []string{
	"context length",
	"context window",
	"maximum context",
	"too many tokens",
	"prompt is too long",
	"reduce the length",
	"exceeds the maximum",
	"input is too long",
}

var overflowExclusions = []string{
	"rate limit",
	"too many requests",
	"service unavailable",
}

// IsContextOverflow reports whether a message says the conversation no longer
// fits.
func IsContextOverflow(message string) bool {
	lowered := strings.ToLower(message)
	for _, phrase := range overflowExclusions {
		if strings.Contains(lowered, phrase) {
			return false
		}
	}
	for _, phrase := range overflowPhrases {
		if strings.Contains(lowered, phrase) {
			return true
		}
	}
	return false
}

var policyPhrases = []string{
	"content policy",
	"content filter",
	"safety",
	"blocked by",
}

func isContentPolicy(message string) bool {
	lowered := strings.ToLower(message)
	for _, phrase := range policyPhrases {
		if strings.Contains(lowered, phrase) {
			return true
		}
	}
	return false
}
