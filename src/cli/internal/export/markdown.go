package export

import (
	"fmt"
	"io"
	"strings"
)

// writeMarkdown is the format a result is pasted into a document or a message
// as. A cell is written on one line, because a markdown table has no way to
// hold a second one, and the bar that separates columns is escaped so a value
// containing one does not become two columns.
type writeMarkdown struct {
	out     io.Writer
	columns int
	err     error
}

func newMarkdown(out io.Writer) Writer { return &writeMarkdown{out: out} }

func (w *writeMarkdown) Head(columns []string) error {
	w.columns = len(columns)
	if err := w.line(columns); err != nil {
		return err
	}
	rule := make([]string, 0, len(columns))
	for range columns {
		rule = append(rule, "---")
	}
	return w.line(rule)
}

func (w *writeMarkdown) Row(values []any) error {
	cells := make([]string, 0, w.columns)
	for i := range w.columns {
		var value any
		if i < len(values) {
			value = values[i]
		}
		cells = append(cells, escape(Text(value)))
	}
	return w.line(cells)
}

func (w *writeMarkdown) Close() error { return w.err }

func (w *writeMarkdown) line(cells []string) error {
	if w.err != nil {
		return w.err
	}
	text := "| " + strings.Join(cells, " | ") + " |\n"
	if _, err := io.WriteString(w.out, text); err != nil {
		w.err = fmt.Errorf("write a row: %w", err)
	}
	return w.err
}

func escape(text string) string {
	text = strings.ReplaceAll(text, `|`, `\|`)
	text = strings.ReplaceAll(text, "\r\n", "<br>")
	text = strings.ReplaceAll(text, "\n", "<br>")
	return text
}
