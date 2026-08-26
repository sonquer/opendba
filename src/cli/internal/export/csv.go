package export

import (
	"encoding/csv"
	"fmt"
	"io"
)

type writeCSV struct{ out *csv.Writer }

func newCSV(out io.Writer) Writer { return &writeCSV{out: csv.NewWriter(out)} }

func (w *writeCSV) Head(columns []string) error {
	if err := w.out.Write(columns); err != nil {
		return fmt.Errorf("write the column names: %w", err)
	}
	return nil
}

func (w *writeCSV) Row(values []any) error {
	cells := make([]string, 0, len(values))
	for _, value := range values {
		cells = append(cells, Text(value))
	}
	if err := w.out.Write(cells); err != nil {
		return fmt.Errorf("write a row: %w", err)
	}
	return nil
}

func (w *writeCSV) Close() error {
	w.out.Flush()
	if err := w.out.Error(); err != nil {
		return fmt.Errorf("finish the file: %w", err)
	}
	return nil
}
