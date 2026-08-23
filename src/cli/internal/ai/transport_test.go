package ai

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type step struct {
	response *http.Response
	err      error
}

type fakeDoer struct {
	steps    []step
	calls    int
	requests []*http.Request
}

func (f *fakeDoer) Do(req *http.Request) (*http.Response, error) {
	f.requests = append(f.requests, req)
	if f.calls >= len(f.steps) {
		return nil, fmt.Errorf("unexpected call %d", f.calls+1)
	}
	next := f.steps[f.calls]
	f.calls++
	return next.response, next.err
}

type countingBody struct {
	reader io.Reader
	closed int
}

func (c *countingBody) Read(p []byte) (int, error) { return c.reader.Read(p) }

func (c *countingBody) Close() error {
	c.closed++
	return nil
}

// replied builds one scripted reply. It returns a step rather than a response
// so that a helper in a test is not mistaken for a request whose body the test
// forgot to close.
func replied(status int, body string, header http.Header) (step, *countingBody) {
	tracked := &countingBody{reader: strings.NewReader(body)}
	if header == nil {
		header = http.Header{}
	}
	return step{response: &http.Response{StatusCode: status, Body: tracked, Header: header}}, tracked
}

func request() Build {
	return func() (*http.Request, error) {
		return http.NewRequest(http.MethodPost, "https://example.test/v1/chat", strings.NewReader("{}"))
	}
}

type recorder struct {
	waits []time.Duration
	err   error
}

func (r *recorder) sleep(_ context.Context, d time.Duration) error {
	r.waits = append(r.waits, d)
	return r.err
}

func transport(doer *fakeDoer, sleep *recorder) *Transport {
	tr := NewTransport(doer, "claude")
	tr.Backoff = time.Second
	tr.Sleep = sleep.sleep
	return tr
}

func TestTransportSucceedsFirstTime(t *testing.T) {
	ok, _ := replied(http.StatusOK, "hello", nil)
	doer := &fakeDoer{steps: []step{ok}}
	sleep := &recorder{}

	response, err := transport(doer, sleep).Do(context.Background(), request())
	if err != nil {
		t.Fatalf("Do() error = %v", err)
	}
	defer response.Body.Close()
	if doer.calls != 1 {
		t.Fatalf("calls = %d, want 1", doer.calls)
	}
	if len(sleep.waits) != 0 {
		t.Fatalf("waited %v on a request that worked", sleep.waits)
	}
}

func TestTransportRetriesThenSucceeds(t *testing.T) {
	broken, brokenBody := replied(http.StatusInternalServerError, "upstream on fire", nil)
	ok, _ := replied(http.StatusOK, "hello", nil)
	doer := &fakeDoer{steps: []step{broken, ok}}
	sleep := &recorder{}

	response, err := transport(doer, sleep).Do(context.Background(), request())
	if err != nil {
		t.Fatalf("Do() error = %v", err)
	}
	defer response.Body.Close()
	if doer.calls != 2 {
		t.Fatalf("calls = %d, want 2", doer.calls)
	}
	if brokenBody.closed != 1 {
		t.Fatalf("the failed response body was closed %d times, want 1", brokenBody.closed)
	}
	if len(sleep.waits) != 1 || sleep.waits[0] != time.Second {
		t.Fatalf("waits = %v, want one second", sleep.waits)
	}
}

func TestTransportDoesNotRetryWhatCannotSucceed(t *testing.T) {
	denied, _ := replied(http.StatusUnauthorized, "invalid x-api-key", nil)
	doer := &fakeDoer{steps: []step{denied}}
	sleep := &recorder{}

	response, err := transport(doer, sleep).Do(context.Background(), request())
	if response != nil {
		response.Body.Close()
		t.Fatal("a failed request must not return a response")
	}
	if doer.calls != 1 {
		t.Fatalf("calls = %d, want 1", doer.calls)
	}
	reason, ok := ReasonOf(err)
	if !ok || reason != ReasonAuth {
		t.Fatalf("reason = %q, want auth", reason)
	}
	var failure *Error
	errors.As(err, &failure)
	if failure.Status != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", failure.Status)
	}
	if !strings.Contains(failure.Message, "invalid x-api-key") {
		t.Fatalf("message = %q, want the body", failure.Message)
	}
}

func TestTransportHonoursRetryAfter(t *testing.T) {
	cases := map[string]struct {
		header http.Header
		want   time.Duration
	}{
		"seconds": {
			header: http.Header{"Retry-After": []string{"3"}},
			want:   3 * time.Second,
		},
		"capped": {
			header: http.Header{"Retry-After": []string{"600"}},
			want:   maxRetryWaitTime,
		},
		"nonsense falls back to the backoff": {
			header: http.Header{"Retry-After": []string{"soon"}},
			want:   time.Second,
		},
		"absent falls back to the backoff": {
			header: http.Header{},
			want:   time.Second,
		},
		"a date in the past falls back": {
			header: http.Header{"Retry-After": []string{"Mon, 02 Jan 2006 15:04:05 GMT"}},
			want:   time.Second,
		},
	}
	for name, test := range cases {
		t.Run(name, func(t *testing.T) {
			limited, _ := replied(http.StatusTooManyRequests, "slow down", test.header)
			ok, _ := replied(http.StatusOK, "hello", nil)
			doer := &fakeDoer{steps: []step{limited, ok}}
			sleep := &recorder{}

			response, err := transport(doer, sleep).Do(context.Background(), request())
			if err != nil {
				t.Fatalf("Do() error = %v", err)
			}
			defer response.Body.Close()
			if len(sleep.waits) != 1 || sleep.waits[0] != test.want {
				t.Fatalf("waits = %v, want %v", sleep.waits, test.want)
			}
		})
	}
}

func TestTransportRetryAfterDate(t *testing.T) {
	when := time.Now().Add(4 * time.Second).UTC().Format(http.TimeFormat)
	limited, _ := replied(http.StatusTooManyRequests, "slow down", http.Header{"Retry-After": []string{when}})
	ok, _ := replied(http.StatusOK, "hello", nil)
	doer := &fakeDoer{steps: []step{limited, ok}}
	sleep := &recorder{}

	response, err := transport(doer, sleep).Do(context.Background(), request())
	if err != nil {
		t.Fatalf("Do() error = %v", err)
	}
	defer response.Body.Close()
	if len(sleep.waits) != 1 || sleep.waits[0] <= 0 || sleep.waits[0] > 5*time.Second {
		t.Fatalf("waits = %v, want a wait derived from the date", sleep.waits)
	}
}

func TestTransportGivesUp(t *testing.T) {
	steps := make([]step, defaultAttempts)
	for i := range steps {
		steps[i], _ = replied(http.StatusServiceUnavailable, "try later", nil)
	}
	doer := &fakeDoer{steps: steps}
	sleep := &recorder{}

	response, err := transport(doer, sleep).Do(context.Background(), request())
	if response != nil {
		response.Body.Close()
		t.Fatal("a failed request must not return a response")
	}
	if doer.calls != defaultAttempts {
		t.Fatalf("calls = %d, want %d", doer.calls, defaultAttempts)
	}
	if len(sleep.waits) != defaultAttempts-1 {
		t.Fatalf("waits = %v, want %d", sleep.waits, defaultAttempts-1)
	}
	if sleep.waits[len(sleep.waits)-1] != 4*time.Second {
		t.Fatalf("last wait = %v, want the backoff to double", sleep.waits[len(sleep.waits)-1])
	}
	if reason, _ := ReasonOf(err); reason != ReasonProvider {
		t.Fatalf("reason = %q, want provider", reason)
	}
}

func TestTransportBuildFailure(t *testing.T) {
	doer := &fakeDoer{}
	sleep := &recorder{}
	broken := errors.New("bad url")

	response, err := transport(doer, sleep).Do(context.Background(), func() (*http.Request, error) {
		return nil, broken
	})
	if response != nil {
		response.Body.Close()
		t.Fatal("a failed request must not return a response")
	}

	if !errors.Is(err, broken) {
		t.Fatalf("Do() error = %v, want it to wrap %v", err, broken)
	}
	if reason, _ := ReasonOf(err); reason != ReasonRequest {
		t.Fatalf("reason = %q, want request", reason)
	}
	if doer.calls != 0 {
		t.Fatal("a request that could not be built must not be sent")
	}
}

func TestTransportSendFailureIsRetried(t *testing.T) {
	refused := errors.New("connection refused")
	ok, _ := replied(http.StatusOK, "hello", nil)
	doer := &fakeDoer{steps: []step{{err: refused}, ok}}
	sleep := &recorder{}

	response, err := transport(doer, sleep).Do(context.Background(), request())
	if err != nil {
		t.Fatalf("Do() error = %v", err)
	}
	defer response.Body.Close()
	if doer.calls != 2 {
		t.Fatalf("calls = %d, want 2", doer.calls)
	}
}

func TestTransportStopsWhenCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	doer := &fakeDoer{steps: []step{{err: errors.New("cancelled by transport")}}}
	sleep := &recorder{}

	response, err := transport(doer, sleep).Do(ctx, request())
	if response != nil {
		response.Body.Close()
		t.Fatal("a failed request must not return a response")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Do() error = %v, want context.Canceled", err)
	}
}

func TestTransportStopsWhenThePauseIsCancelled(t *testing.T) {
	broken, _ := replied(http.StatusInternalServerError, "boom", nil)
	ok, _ := replied(http.StatusOK, "hello", nil)
	doer := &fakeDoer{steps: []step{broken, ok}}
	sleep := &recorder{err: context.Canceled}

	response, err := transport(doer, sleep).Do(context.Background(), request())
	if response != nil {
		response.Body.Close()
		t.Fatal("a failed request must not return a response")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Do() error = %v, want context.Canceled", err)
	}
	if doer.calls != 1 {
		t.Fatalf("calls = %d, want the retry to be abandoned", doer.calls)
	}
}

func TestTransportDefaults(t *testing.T) {
	denied, _ := replied(http.StatusUnauthorized, "", nil)
	doer := &fakeDoer{steps: []step{denied}}
	tr := NewTransport(doer, "claude")
	tr.Attempts = 0

	response, err := tr.Do(context.Background(), request())
	if response != nil {
		response.Body.Close()
		t.Fatal("a failed request must not return a response")
	}
	if doer.calls != 1 {
		t.Fatalf("calls = %d, want a floor of one attempt", doer.calls)
	}
	var failure *Error
	errors.As(err, &failure)
	if failure.Message != "http 401" {
		t.Fatalf("message = %q, want the status when the body is empty", failure.Message)
	}
}

func TestTransportSleepsForRealWhenNotReplaced(t *testing.T) {
	broken, _ := replied(http.StatusInternalServerError, "boom", nil)
	ok, _ := replied(http.StatusOK, "hello", nil)
	doer := &fakeDoer{steps: []step{broken, ok}}
	tr := NewTransport(doer, "claude")
	tr.Backoff = time.Nanosecond
	tr.Sleep = nil

	response, err := tr.Do(context.Background(), request())
	if err != nil {
		t.Fatalf("Do() error = %v", err)
	}
	defer response.Body.Close()
	if doer.calls != 2 {
		t.Fatalf("calls = %d, want 2", doer.calls)
	}
}

func TestTransportTruncatesALongBody(t *testing.T) {
	long := strings.Repeat("x", 900)
	denied, _ := replied(http.StatusBadRequest, long, nil)
	doer := &fakeDoer{steps: []step{denied}}
	sleep := &recorder{}

	response, err := transport(doer, sleep).Do(context.Background(), request())
	if response != nil {
		response.Body.Close()
		t.Fatal("a failed request must not return a response")
	}
	var failure *Error
	errors.As(err, &failure)
	if len(failure.Message) != 400 {
		t.Fatalf("message length = %d, want it truncated to 400", len(failure.Message))
	}
}

func TestTransportDefaultBackoff(t *testing.T) {
	tr := &Transport{Backoff: 0}
	if got := tr.delay(1, nil); got != defaultBackoff {
		t.Fatalf("delay() = %v, want the default backoff", got)
	}
}

func TestWaitStopsWhenCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := wait(ctx, time.Hour); !errors.Is(err, context.Canceled) {
		t.Fatalf("wait() error = %v, want context.Canceled", err)
	}
}
