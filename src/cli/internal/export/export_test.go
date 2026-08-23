package export

import (
	"archive/zip"
	"bytes"
	"errors"
	"io"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/xuri/excelize/v2"
)

// row is the value matrix every writer is held to.
func row() []any {
	return []any{
		nil,
		"plain",
		`quoted "and" comma, and a pipe |`,
		"two\nlines",
		[]byte("bytes as text"),
		[]byte{0xff, 0xfe, 0x00},
		time.Date(2026, 8, 23, 14, 5, 6, 0, time.UTC),
		int64(-42),
		3.5,
		true,
	}
}

func columns() []string {
	return []string{"nothing", "plain", "punctuated", "wrapped", "text bytes",
		"raw bytes", "when", "count(*)", "ratio", "yes"}
}

func written(t *testing.T, format Format, rows ...[]any) string {
	t.Helper()
	var held bytes.Buffer
	writer, err := New(format, &held, Options{TempDir: t.TempDir()})
	if err != nil {
		t.Fatalf("New(%s): %v", format, err)
	}
	if err := writer.Head(columns()); err != nil {
		t.Fatalf("Head: %v", err)
	}
	for _, values := range rows {
		if err := writer.Row(values); err != nil {
			t.Fatalf("Row: %v", err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	return held.String()
}

// Every format is offered, is named by its extension, and is found by name.
func TestEveryFormatIsNamedAndFound(t *testing.T) {
	for _, format := range Formats() {
		found, ok := Named(string(format))
		if !ok || found != format {
			t.Errorf("Named(%q) = %q, %v", format, found, ok)
		}
		if format.Extension() == "" {
			t.Errorf("%q has no extension", format)
		}
	}
	if FormatMarkdown.Extension() != "md" {
		t.Errorf("markdown is written to .%s", FormatMarkdown.Extension())
	}
	if _, ok := Named("parquet"); ok {
		t.Error("a format that does not exist must not be found")
	}
	if _, err := New("parquet", &bytes.Buffer{}, Options{}); err == nil {
		t.Error("and must not be built")
	}
}

// A value keeps everything a file can hold: nothing is truncated, a newline
// survives, and bytes that are not text do not end up raw in the file.
func TestAValueIsWrittenWholeOrNotAtAll(t *testing.T) {
	for _, want := range []struct {
		name  string
		value any
		text  string
	}{
		{"nothing", nil, ""},
		{"a string", "plain", "plain"},
		{"a long string", strings.Repeat("x", 200), strings.Repeat("x", 200)},
		{"two lines", "a\nb", "a\nb"},
		{"text bytes", []byte("text"), "text"},
		{"bytes that are not text", []byte{0xff, 0xfe}, "//4="},
		{"a time", time.Date(2026, 8, 23, 14, 5, 6, 0, time.UTC), "2026-08-23T14:05:06Z"},
		{"a whole number", int64(-42), "-42"},
		{"a plain int", 7, "7"},
		{"a small int", int32(7), "7"},
		{"a fraction", 3.5, "3.5"},
		{"a small fraction", float32(2.5), "2.5"},
		{"a truth", true, "true"},
		{"something else", struct{ A int }{1}, "{1}"},
	} {
		t.Run(want.name, func(t *testing.T) {
			if got := Text(want.value); got != want.text {
				t.Errorf("Text = %q, want %q", got, want.text)
			}
		})
	}
}

// A value that a document can hold as itself stays itself, so a number is not
// written as a string.
func TestANumberStaysANumber(t *testing.T) {
	for _, want := range []struct {
		name  string
		value any
		held  any
	}{
		{"nothing", nil, nil},
		{"a whole number", int64(3), int64(3)},
		{"a fraction", 3.5, 3.5},
		{"a truth", true, true},
		{"a string", "a", "a"},
		{"text bytes", []byte("a"), "a"},
		{"a time", time.Date(2026, 8, 23, 0, 0, 0, 0, time.UTC), "2026-08-23T00:00:00Z"},
		{"something else", struct{ A int }{1}, "{1}"},
	} {
		t.Run(want.name, func(t *testing.T) {
			if got := Value(want.value); got != want.held {
				t.Errorf("Value = %#v, want %#v", got, want.held)
			}
		})
	}
}

func TestCSVQuotesWhatItMust(t *testing.T) {
	file := written(t, FormatCSV, row())
	for _, want := range []string{
		`nothing,plain,punctuated`,
		`"quoted ""and"" comma, and a pipe |"`,
		`"two` + "\n" + `lines"`,
		`bytes as text`,
		`//4A`,
		`2026-08-23T14:05:06Z`,
		`-42`,
		`3.5`,
		`true`,
	} {
		if !strings.Contains(file, want) {
			t.Errorf("the file must hold %q:\n%s", want, file)
		}
	}
}

func TestJSONKeepsTheColumnsInOrder(t *testing.T) {
	file := written(t, FormatJSON, row())
	if !strings.HasPrefix(file, "[\n  {") || !strings.HasSuffix(file, "}\n]\n") {
		t.Errorf("the file must be an array of objects:\n%s", file)
	}
	if at := strings.Index(file, `"nothing": null`); at < 0 {
		t.Errorf("a null must be a null:\n%s", file)
	}
	if !strings.Contains(file, `"count(*)": -42`) {
		t.Errorf("a number must not be written as a string:\n%s", file)
	}
	if !strings.Contains(file, `"yes": true`) {
		t.Errorf("nor a truth:\n%s", file)
	}
	if strings.Contains(file, `\u0026`) || strings.Contains(file, `\u003c`) {
		t.Errorf("this is a file rather than a page:\n%s", file)
	}
	if at, then := strings.Index(file, `"nothing"`), strings.Index(file, `"plain"`); at > then {
		t.Errorf("the columns must be written in the order they came in:\n%s", file)
	}
}

func TestJSONWithNoRowsIsStillAnArray(t *testing.T) {
	if file := written(t, FormatJSON); file != "[]\n" {
		t.Errorf("file = %q", file)
	}
}

func TestXMLNamesAColumnInAnAttribute(t *testing.T) {
	file := written(t, FormatXML, row())
	if !strings.HasPrefix(file, `<?xml version="1.0" encoding="UTF-8"?>`) {
		t.Errorf("the file must declare itself:\n%s", file)
	}
	if !strings.Contains(file, `<field name="count(*)">-42</field>`) {
		t.Errorf("a name XML would not allow on an element must go in an attribute:\n%s", file)
	}
	if !strings.Contains(file, `<field name="nothing" null="true">`) {
		t.Errorf("a null must say it is one:\n%s", file)
	}
	if !strings.Contains(file, "&#34;and&#34;") && !strings.Contains(file, "&#x22;and&#x22;") {
		t.Errorf("a quote must be escaped:\n%s", file)
	}
}

func TestMarkdownKeepsARowOnOneLine(t *testing.T) {
	file := written(t, FormatMarkdown, row())
	lines := strings.Split(strings.TrimRight(file, "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("lines = %d, a header, a rule and a row:\n%s", len(lines), file)
	}
	if !strings.Contains(lines[1], "---") {
		t.Errorf("the second line is the rule:\n%s", file)
	}
	if !strings.Contains(lines[2], `\|`) {
		t.Errorf("a bar in a value must not become a column:\n%s", file)
	}
	if !strings.Contains(lines[2], "two<br>lines") {
		t.Errorf("a second line must be folded rather than dropped:\n%s", file)
	}
}

func TestXLSXHoldsItsValuesAsThemselves(t *testing.T) {
	var held bytes.Buffer
	writer, err := New(FormatXLSX, &held, Options{TempDir: t.TempDir(), Sheet: "Rows"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := writer.Head(columns()); err != nil {
		t.Fatalf("Head: %v", err)
	}
	if err := writer.Row(row()); err != nil {
		t.Fatalf("Row: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	book, err := excelize.OpenReader(bytes.NewReader(held.Bytes()))
	if err != nil {
		t.Fatalf("OpenReader: %v", err)
	}
	defer func() { _ = book.Close() }()
	if sheets := book.GetSheetList(); len(sheets) != 1 || sheets[0] != "Rows" {
		t.Fatalf("sheets = %v", sheets)
	}
	rows, err := book.GetRows("Rows")
	if err != nil {
		t.Fatalf("GetRows: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("rows = %d, a header and a row", len(rows))
	}
	if rows[0][7] != "count(*)" {
		t.Errorf("the header must hold the column names: %v", rows[0])
	}
	for at, want := range map[int]string{1: "plain", 7: "-42", 8: "3.5", 9: "TRUE"} {
		if rows[1][at] != want {
			t.Errorf("cell %d = %q, want %q", at, rows[1][at], want)
		}
	}
}

// A result longer than a sheet rolls onto the next one rather than stopping, so
// a file never quietly holds less than was exported.
func TestXLSXRollsOntoAnotherSheet(t *testing.T) {
	var held bytes.Buffer
	writer, err := New(FormatXLSX, &held, Options{TempDir: t.TempDir()})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	sheet := writer.(*writeXLSX)
	if err := sheet.Head([]string{"n"}); err != nil {
		t.Fatalf("Head: %v", err)
	}
	sheet.row = sheetRows - 1
	for i := range 3 {
		if err := sheet.Row([]any{int64(i)}); err != nil {
			t.Fatalf("Row: %v", err)
		}
	}
	if err := sheet.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	book, err := excelize.OpenReader(bytes.NewReader(held.Bytes()))
	if err != nil {
		t.Fatalf("OpenReader: %v", err)
	}
	defer func() { _ = book.Close() }()
	if sheets := book.GetSheetList(); len(sheets) != 2 {
		t.Fatalf("sheets = %v, a result past the end of one rolls onto another", sheets)
	}
}

// Whatever the file is being written to can stop taking it, at any point, and
// every writer has to say so rather than carry on writing into the dark. How
// many writes a format makes is counted rather than guessed, because a writer
// that buffers makes far fewer of them than it is asked to.
func TestAWriterSaysSoWhereverTheFileStopsTakingIt(t *testing.T) {
	for _, format := range []Format{FormatCSV, FormatJSON, FormatXML, FormatMarkdown} {
		t.Run(string(format), func(t *testing.T) {
			counted := &failing{after: math.MaxInt32}
			drive(t, format, counted)
			if counted.written == 0 {
				t.Fatalf("%s wrote nothing at all", format)
			}
			for after := range counted.written {
				out := &failing{after: after}
				if !drive(t, format, out) {
					t.Errorf("failing after %d of %d writes must be an error somewhere",
						after, counted.written)
				}
			}
		})
	}
}

// drive writes a whole file and says whether anything refused. The rows are
// long on purpose: a writer that buffers only meets a broken file once it has
// enough to flush.
func drive(t *testing.T, format Format, out io.Writer) bool {
	t.Helper()
	writer, err := New(format, out, Options{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	long := append(row(), nil)
	long[1] = strings.Repeat("x", 5000)
	failed := writer.Head(columns()) != nil
	failed = writer.Row(long) != nil || failed
	failed = writer.Row(long) != nil || failed
	return writer.Close() != nil || failed
}

// A zip that cannot be written is the one failure the spreadsheet writer can
// only meet at the end.
func TestXLSXSaysSoWhenTheFileWillNotTakeIt(t *testing.T) {
	writer, err := New(FormatXLSX, &failing{}, Options{TempDir: t.TempDir()})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := writer.Head([]string{"n"}); err != nil {
		t.Fatalf("Head: %v", err)
	}
	if err := writer.Close(); err == nil {
		t.Error("Close must say the file could not be written")
	}
}

// A cell past the last column a spreadsheet has is a cell it cannot place.
func TestXLSXSaysSoWhenARowWillNotFit(t *testing.T) {
	var held bytes.Buffer
	writer, err := New(FormatXLSX, &held, Options{TempDir: t.TempDir()})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	sheet := writer.(*writeXLSX)
	sheet.row = math.MaxInt32
	if err := sheet.put([]any{"x"}); err == nil {
		t.Error("a row that cannot be placed must say so")
	}
}

// A written file is a zip, which is what says the spreadsheet is one.
func TestXLSXIsAZip(t *testing.T) {
	var held bytes.Buffer
	writer, err := New(FormatXLSX, &held, Options{TempDir: t.TempDir()})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := writer.Head([]string{"n"}); err != nil {
		t.Fatalf("Head: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, err := zip.NewReader(bytes.NewReader(held.Bytes()), int64(held.Len())); err != nil {
		t.Errorf("the file must be a zip: %v", err)
	}
}

// failing is somewhere to write that stops taking bytes after a while, so that
// every place a writer can meet a broken file is reached rather than only the
// first one.
type failing struct {
	after   int
	written int
}

func (f *failing) Write(raw []byte) (int, error) {
	if f.written >= f.after {
		return 0, errors.New("the disk is full")
	}
	f.written++
	return len(raw), nil
}

// A header long enough to fill the buffer meets a broken file the way a row
// does, which is the only way the header of a buffered format ever can.
func TestALongHeaderMeetsABrokenFileToo(t *testing.T) {
	for _, format := range []Format{FormatCSV, FormatJSON, FormatXML, FormatMarkdown} {
		t.Run(string(format), func(t *testing.T) {
			writer, err := New(format, &failing{}, Options{})
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			if writer.Head([]string{strings.Repeat("n", 9000)}) == nil {
				t.Error("a header that will not fit in the buffer must be written, and fail")
			}
		})
	}
}

// A number a document has no way to hold is a failure rather than a file with
// something invalid in it.
func TestANumberJSONCannotHoldIsRefused(t *testing.T) {
	var held bytes.Buffer
	writer, err := New(FormatJSON, &held, Options{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := writer.Head([]string{"ratio"}); err != nil {
		t.Fatalf("Head: %v", err)
	}
	if err := writer.Row([]any{math.Inf(1)}); err == nil {
		t.Error("infinity is not a number JSON can hold")
	}
}

// A sheet a spreadsheet will not accept the name of is refused before anything
// is written.
func TestXLSXRefusesASheetNameItCannotUse(t *testing.T) {
	if _, err := New(FormatXLSX, &bytes.Buffer{},
		Options{TempDir: t.TempDir(), Sheet: strings.Repeat("x", 40)}); err == nil {
		t.Error("a name longer than a sheet name may be must be refused")
	}
	if _, err := New(FormatXLSX, &bytes.Buffer{},
		Options{TempDir: t.TempDir(), Sheet: "a:b"}); err == nil {
		t.Error("and so must one holding a character a sheet name may not")
	}
}

// A sheet that cannot be added is a failure rather than rows quietly going
// nowhere.
func TestXLSXSaysSoWhenItCannotRollOntoAnotherSheet(t *testing.T) {
	writer, err := New(FormatXLSX, &bytes.Buffer{}, Options{TempDir: t.TempDir()})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	sheet := writer.(*writeXLSX)
	if err := sheet.Head([]string{"n"}); err != nil {
		t.Fatalf("Head: %v", err)
	}
	sheet.name = strings.Repeat("x", 30)
	sheet.row = sheetRows
	if err := sheet.Row([]any{int64(1)}); err == nil {
		t.Error("a sheet that cannot be named must be an error")
	}
}
