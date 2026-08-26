package ai

import (
	"errors"
	"io"
	"strings"
	"testing"
)

func stream(text string) (*Events, *countingBody) {
	body := &countingBody{reader: strings.NewReader(text)}
	return NewEvents(body), body
}

func drain(t *testing.T, events *Events) []Event {
	t.Helper()
	var seen []Event
	for {
		event, err := events.Next()
		if errors.Is(err, io.EOF) {
			return seen
		}
		if err != nil {
			t.Fatalf("Next() error = %v", err)
		}
		seen = append(seen, event)
	}
}

func TestEventsNext(t *testing.T) {
	cases := map[string]struct {
		text string
		want []Event
	}{
		"named event": {
			text: "event: content_block_delta\ndata: {\"text\":\"hi\"}\n\n",
			want: []Event{{Name: "content_block_delta", Data: `{"text":"hi"}`}},
		},
		"data only": {
			text: "data: {\"choices\":[]}\n\n",
			want: []Event{{Data: `{"choices":[]}`}},
		},
		"two events": {
			text: "data: one\n\ndata: two\n\n",
			want: []Event{{Data: "one"}, {Data: "two"}},
		},
		"data lines are joined": {
			text: "data: {\ndata: \"a\": 1\ndata: }\n\n",
			want: []Event{{Data: "{\n\"a\": 1\n}"}},
		},
		"comments are skipped": {
			text: ": ping\ndata: one\n\n",
			want: []Event{{Data: "one"}},
		},
		"keep alive blank lines are skipped": {
			text: "\n\n\ndata: one\n\n",
			want: []Event{{Data: "one"}},
		},
		"carriage returns": {
			text: "event: done\r\ndata: [DONE]\r\n\r\n",
			want: []Event{{Name: "done", Data: "[DONE]"}},
		},
		"no trailing blank line": {
			text: "data: last",
			want: []Event{{Data: "last"}},
		},
		"unknown fields are ignored": {
			text: "id: 42\nretry: 100\ndata: one\n\n",
			want: []Event{{Data: "one"}},
		},
		"a field with no value": {
			text: "data\n\n",
			want: []Event{{Data: ""}},
		},
		"empty stream": {
			text: "",
			want: nil,
		},
		"only comments": {
			text: ": ping\n: ping\n",
			want: nil,
		},
	}
	for name, test := range cases {
		t.Run(name, func(t *testing.T) {
			events, _ := stream(test.text)
			got := drain(t, events)
			if len(got) != len(test.want) {
				t.Fatalf("read %d events, want %d: %#v", len(got), len(test.want), got)
			}
			for i, want := range test.want {
				if got[i] != want {
					t.Fatalf("event %d = %#v, want %#v", i, got[i], want)
				}
			}
		})
	}
}

func TestEventsCloses(t *testing.T) {
	events, body := stream("data: one\n\n")
	if err := events.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if body.closed != 1 {
		t.Fatalf("body closed %d times, want 1", body.closed)
	}
}

func TestEventsPropagatesReadFailure(t *testing.T) {
	broken := errors.New("connection reset")
	events := NewEvents(&countingBody{reader: iotest(broken)})
	if _, err := events.Next(); !errors.Is(err, broken) {
		t.Fatalf("Next() error = %v, want %v", err, broken)
	}
}

type failingReader struct{ err error }

func (f failingReader) Read([]byte) (int, error) { return 0, f.err }

func iotest(err error) io.Reader { return failingReader{err: err} }
