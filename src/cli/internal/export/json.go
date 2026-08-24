package export

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
)

// writeJSON writes an array of objects a row at a time rather than building the
// whole document and marshalling it, so the size of a result is the size of the
// file and not also the size of the memory it took to write it.
type writeJSON struct {
	out     io.Writer
	columns []string
	written int
	err     error
}

func newJSON(out io.Writer) Writer { return &writeJSON{out: out} }

func (w *writeJSON) Head(columns []string) error {
	w.columns = columns
	return w.write("[")
}

func (w *writeJSON) Row(values []any) error {
	var row bytes.Buffer
	if w.written > 0 {
		row.WriteString(",")
	}
	row.WriteString("\n  {")
	for i, name := range w.columns {
		if i > 0 {
			row.WriteString(", ")
		}
		field, err := encode(name)
		if err != nil {
			return err
		}
		row.Write(field)
		row.WriteString(": ")
		var value any
		if i < len(values) {
			value = Value(values[i])
		}
		held, err := encode(value)
		if err != nil {
			return err
		}
		row.Write(held)
	}
	row.WriteString("}")
	w.written++
	return w.write(row.String())
}

func (w *writeJSON) Close() error {
	if w.written == 0 {
		return w.write("]\n")
	}
	return w.write("\n]\n")
}

func (w *writeJSON) write(text string) error {
	if w.err != nil {
		return w.err
	}
	if _, err := io.WriteString(w.out, text); err != nil {
		w.err = fmt.Errorf("write the file: %w", err)
		return w.err
	}
	return nil
}

// encode writes one value without turning the characters HTML cares about into
// escapes, because this is a file rather than a page.
func encode(value any) ([]byte, error) {
	var held bytes.Buffer
	encoder := json.NewEncoder(&held)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return nil, fmt.Errorf("write a value: %w", err)
	}
	return bytes.TrimRight(held.Bytes(), "\n"), nil
}
