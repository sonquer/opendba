// Package shot turns what a terminal drew into something that can be looked at
// after the run that drew it has gone.
package shot

import (
	"encoding/json"
	"fmt"
	"io"
	"time"
)

type header struct {
	Version   int               `json:"version"`
	Width     int               `json:"width"`
	Height    int               `json:"height"`
	Timestamp int64             `json:"timestamp"`
	Title     string            `json:"title"`
	Env       map[string]string `json:"env"`
}

// Frame is one thing the program wrote and when it wrote it.
type Frame struct {
	At   time.Duration
	Data []byte
}

// Cast writes a recording in the asciinema v2 format, which is a header on the
// first line and one event per line after it, so that a run that failed can be
// played back rather than described.
func Cast(out io.Writer, title string, width, height int, stamp time.Time, frames []Frame) error {
	head, err := json.Marshal(header{
		Version:   2,
		Width:     width,
		Height:    height,
		Timestamp: stamp.Unix(),
		Title:     title,
		Env:       map[string]string{"TERM": "xterm-256color", "SHELL": ""},
	})
	if err != nil {
		return fmt.Errorf("write the recording header: %w", err)
	}
	if _, err := fmt.Fprintf(out, "%s\n", head); err != nil {
		return fmt.Errorf("write the recording header: %w", err)
	}
	for _, frame := range frames {
		event, err := json.Marshal([]any{frame.At.Seconds(), "o", string(frame.Data)})
		if err != nil {
			return fmt.Errorf("write a recorded frame: %w", err)
		}
		if _, err := fmt.Fprintf(out, "%s\n", event); err != nil {
			return fmt.Errorf("write a recorded frame: %w", err)
		}
	}
	return nil
}
