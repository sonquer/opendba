package export

import (
	"fmt"
	"io"

	"github.com/xuri/excelize/v2"
)

// sheetRows is what a sheet holds. It is the format's own limit rather than a
// choice: past it a workbook is not a workbook any more.
const sheetRows = excelize.TotalRows

// defaultSheet is what the one sheet is called when nothing names it.
const defaultSheet = "Result"

// writeXLSX writes a spreadsheet without holding it in memory. excelize's
// stream writer keeps what it has written in a file of its own once it grows,
// which is why it is given somewhere to put it.
//
// A result longer than a sheet rolls onto the next sheet rather than stopping,
// because a file that quietly holds the first million rows of a larger result
// is a file that lies about what was exported.
type writeXLSX struct {
	file    *excelize.File
	stream  *excelize.StreamWriter
	out     io.Writer
	columns []string
	name    string
	sheets  int
	row     int
}

func newXLSX(out io.Writer, options Options) (Writer, error) {
	file := excelize.NewFile(excelize.Options{TmpDir: options.TempDir})
	name := options.Sheet
	if name == "" {
		name = defaultSheet
	}
	writer := &writeXLSX{file: file, out: out, name: name}
	if err := file.SetSheetName(file.GetSheetName(0), name); err != nil {
		return nil, fmt.Errorf("name the sheet: %w", err)
	}
	writer.sheets = 1
	stream, err := file.NewStreamWriter(name)
	if err != nil {
		return nil, fmt.Errorf("start the sheet: %w", err)
	}
	writer.stream = stream
	return writer, nil
}

func (w *writeXLSX) Head(columns []string) error {
	w.columns = columns
	return w.head()
}

// head writes the column names at the top of whichever sheet is being written,
// so a result that rolled onto a second sheet is still readable on its own.
func (w *writeXLSX) head() error {
	w.row = 1
	cells := make([]any, 0, len(w.columns))
	for _, name := range w.columns {
		cells = append(cells, name)
	}
	return w.put(cells)
}

func (w *writeXLSX) Row(values []any) error {
	if w.row >= sheetRows {
		if err := w.rollOver(); err != nil {
			return err
		}
	}
	w.row++
	cells := make([]any, 0, len(values))
	for _, value := range values {
		cells = append(cells, Value(value))
	}
	return w.put(cells)
}

func (w *writeXLSX) put(cells []any) error {
	reference, err := excelize.CoordinatesToCellName(1, w.row)
	if err != nil {
		return fmt.Errorf("place a row: %w", err)
	}
	if err := w.stream.SetRow(reference, cells); err != nil {
		return fmt.Errorf("write a row: %w", err)
	}
	return nil
}

// rollOver starts another sheet, which is what a result longer than a sheet
// needs and what the format gives no other answer to.
func (w *writeXLSX) rollOver() error {
	if err := w.stream.Flush(); err != nil {
		return fmt.Errorf("finish the sheet: %w", err)
	}
	w.sheets++
	name := fmt.Sprintf("%s %d", w.name, w.sheets)
	if _, err := w.file.NewSheet(name); err != nil {
		return fmt.Errorf("start another sheet: %w", err)
	}
	stream, err := w.file.NewStreamWriter(name)
	if err != nil {
		return fmt.Errorf("start another sheet: %w", err)
	}
	w.stream = stream
	return w.head()
}

func (w *writeXLSX) Close() error {
	if err := w.stream.Flush(); err != nil {
		return fmt.Errorf("finish the sheet: %w", err)
	}
	if _, err := w.file.WriteTo(w.out); err != nil {
		return fmt.Errorf("write the file: %w", err)
	}
	if err := w.file.Close(); err != nil {
		return fmt.Errorf("finish the file: %w", err)
	}
	return nil
}
