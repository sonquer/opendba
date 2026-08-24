// Package export writes a result to a file, one row at a time.
package export

import (
	"fmt"
	"io"
)

// Format is a file a result can be written as.
type Format string

const (
	FormatCSV      Format = "csv"
	FormatXLSX     Format = "xlsx"
	FormatJSON     Format = "json"
	FormatXML      Format = "xml"
	FormatMarkdown Format = "markdown"
)

// Formats is every format there is, in the order they are offered.
func Formats() []Format {
	return []Format{FormatCSV, FormatXLSX, FormatJSON, FormatXML, FormatMarkdown}
}

// Extension is what a file of this format is called.
func (f Format) Extension() string {
	if f == FormatMarkdown {
		return "md"
	}
	return string(f)
}

// Named finds a format by name, so a setting or a command line can say one.
func Named(name string) (Format, bool) {
	for _, format := range Formats() {
		if string(format) == name {
			return format, true
		}
	}
	return "", false
}

// Options are the things a writer needs that are not the rows.
type Options struct {
	// Sheet names the one sheet a spreadsheet gets. Empty is the default name.
	Sheet string

	// TempDir is where a writer that cannot hold a whole file in memory puts what
	// it has written so far.
	TempDir string
}

// Writer takes a result one row at a time, so a result larger than memory can
// still be written. Head is called once, before any row.
type Writer interface {
	Head(columns []string) error
	Row(values []any) error
	Close() error
}

// New builds the writer for a format.
func New(format Format, out io.Writer, options Options) (Writer, error) {
	switch format {
	case FormatCSV:
		return newCSV(out), nil
	case FormatJSON:
		return newJSON(out), nil
	case FormatXML:
		return newXML(out), nil
	case FormatMarkdown:
		return newMarkdown(out), nil
	case FormatXLSX:
		return newXLSX(out, options)
	default:
		return nil, fmt.Errorf("there is no %q to write", string(format))
	}
}
