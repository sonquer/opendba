package ai

import (
	"bufio"
	"errors"
	"io"
	"strings"
)

// Event is one server-sent event: the name the provider gave it, and the data
// lines joined back together.
type Event struct {
	Name string
	Data string
}

// Events reads a text/event-stream. Three providers speak this format and only
// disagree about what they put inside the data, so the framing is read once
// here and the meaning is read by each of them.
type Events struct {
	reader *bufio.Reader
	body   io.ReadCloser
}

// NewEvents reads events from a response body, which it closes.
func NewEvents(body io.ReadCloser) *Events {
	return &Events{reader: bufio.NewReader(body), body: body}
}

// Next returns the next event, or io.EOF when the stream is finished. A comment
// and a keep-alive both arrive as an event with nothing in it, and neither is
// returned. A stream that ends without its final blank line still yields what
// it had gathered, because a cut connection is exactly how that happens.
func (e *Events) Next() (Event, error) {
	var event Event
	var data []string
	for {
		line, err := e.reader.ReadString('\n')
		ended := errors.Is(err, io.EOF)
		if err != nil && !ended {
			return Event{}, err
		}
		text := strings.TrimRight(line, "\r\n")
		if text == "" {
			if ended {
				return flushed(event, data)
			}
			if len(data) == 0 && event.Name == "" {
				continue
			}
			return finish(event, data), nil
		}
		if !strings.HasPrefix(text, ":") {
			read(text, &event, &data)
		}
		if ended {
			return flushed(event, data)
		}
	}
}

// Close ends the stream early.
func (e *Events) Close() error { return e.body.Close() }

func read(text string, event *Event, data *[]string) {
	field, value, found := strings.Cut(text, ":")
	if !found {
		field, value = text, ""
	}
	value = strings.TrimPrefix(value, " ")
	switch field {
	case "event":
		event.Name = value
	case "data":
		*data = append(*data, value)
	}
}

func flushed(event Event, data []string) (Event, error) {
	if len(data) == 0 && event.Name == "" {
		return Event{}, io.EOF
	}
	return finish(event, data), nil
}

func finish(event Event, data []string) Event {
	event.Data = strings.Join(data, "\n")
	return event
}
