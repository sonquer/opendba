package shot

import (
	"bytes"
	"encoding/json"
	"image/color"
	"strings"
	"testing"
	"time"
)

func row(text string, fg, bg color.Color) []Cell {
	cells := make([]Cell, 0, len(text))
	for _, letter := range text {
		cells = append(cells, Cell{Content: string(letter), Fg: fg, Bg: bg})
	}
	return cells
}

func TestSVGDrawsTheGridInTheColoursItHad(t *testing.T) {
	red := color.RGBA{R: 0xff, A: 0xff}
	blue := color.RGBA{B: 0x80, A: 0xff}
	grid := [][]Cell{row("hi", red, blue), row("  ", nil, nil)}
	var out bytes.Buffer
	if err := SVG(&out, grid, nil, nil); err != nil {
		t.Fatalf("SVG = %v", err)
	}
	drawn := out.String()
	for _, want := range []string{
		"<svg xmlns=", `viewBox="0 0`, "#ff0000", "#000080", ">hi<", "</svg>",
	} {
		if !strings.Contains(drawn, want) {
			t.Errorf("the picture is missing %q", want)
		}
	}
}

func TestSVGDrawsNoTextForABlankRun(t *testing.T) {
	var out bytes.Buffer
	if err := SVG(&out, [][]Cell{row("   ", nil, nil)}, nil, nil); err != nil {
		t.Fatalf("SVG = %v", err)
	}
	if strings.Contains(out.String(), "<text") {
		t.Error("a run of spaces was drawn as text")
	}
}

func TestSVGCarriesBoldAndUnderlineAndEscapesTheContent(t *testing.T) {
	grid := [][]Cell{{
		{Content: "<", Bold: true},
		{Content: "&", Bold: true},
		{Content: "a", Underline: true},
	}}
	var out bytes.Buffer
	if err := SVG(&out, grid, nil, nil); err != nil {
		t.Fatalf("SVG = %v", err)
	}
	drawn := out.String()
	for _, want := range []string{`font-weight="bold"`, "&lt;&amp;", `text-decoration="underline"`} {
		if !strings.Contains(drawn, want) {
			t.Errorf("the picture is missing %q:\n%s", want, drawn)
		}
	}
}

func TestSVGFallsBackToTheTerminalColours(t *testing.T) {
	white := color.RGBA{R: 0xdc, G: 0xdc, B: 0xdc, A: 0xff}
	var out bytes.Buffer
	if err := SVG(&out, [][]Cell{{{Content: "x"}}}, white, nil); err != nil {
		t.Fatalf("SVG = %v", err)
	}
	if !strings.Contains(out.String(), "#dcdcdc") {
		t.Error("the default foreground was not used")
	}
}

func TestSVGFillsAnEmptyCellWithASpace(t *testing.T) {
	var out bytes.Buffer
	if err := SVG(&out, [][]Cell{{{}, {}}}, nil, nil); err != nil {
		t.Fatalf("SVG = %v", err)
	}
	if !strings.Contains(out.String(), "<svg") {
		t.Error("nothing was drawn")
	}
}

type broken struct{}

func (broken) Write([]byte) (int, error) { return 0, errBroken }

var errBroken = &writeError{}

type writeError struct{}

func (*writeError) Error() string { return "the writer is broken" }

func TestSVGReportsAWriterThatWillNotTake(t *testing.T) {
	if err := SVG(broken{}, [][]Cell{{{Content: "x"}}}, nil, nil); err == nil {
		t.Error("a broken writer was accepted")
	}
}

func TestCastRecordsWhatTheProgramWrote(t *testing.T) {
	var out bytes.Buffer
	frames := []Frame{
		{At: 0, Data: []byte("first")},
		{At: 1500 * time.Millisecond, Data: []byte("second")},
	}
	stamp := time.Unix(1767225600, 0)
	if err := Cast(&out, "editor at 80x24", 80, 24, stamp, frames); err != nil {
		t.Fatalf("Cast = %v", err)
	}
	lines := strings.Split(strings.TrimRight(out.String(), "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("the recording has %d lines", len(lines))
	}
	var head header
	if err := json.Unmarshal([]byte(lines[0]), &head); err != nil {
		t.Fatalf("the header is not JSON: %v", err)
	}
	if head.Version != 2 || head.Width != 80 || head.Height != 24 {
		t.Errorf("header = %#v", head)
	}
	if head.Timestamp != stamp.Unix() || head.Title != "editor at 80x24" {
		t.Errorf("header = %#v", head)
	}
	var event []any
	if err := json.Unmarshal([]byte(lines[2]), &event); err != nil {
		t.Fatalf("an event is not JSON: %v", err)
	}
	if event[0].(float64) != 1.5 || event[1].(string) != "o" || event[2].(string) != "second" {
		t.Errorf("event = %#v", event)
	}
}

func TestCastReportsAWriterThatWillNotTake(t *testing.T) {
	if err := Cast(broken{}, "x", 1, 1, time.Unix(0, 0), nil); err == nil {
		t.Error("a broken writer took the header")
	}
	frames := []Frame{{Data: []byte("x")}}
	if err := Cast(&onlyHeader{}, "x", 1, 1, time.Unix(0, 0), frames); err == nil {
		t.Error("a writer that stopped after the header was accepted")
	}
}

type onlyHeader struct{ wrote bool }

func (o *onlyHeader) Write(p []byte) (int, error) {
	if o.wrote {
		return 0, errBroken
	}
	o.wrote = true
	return len(p), nil
}
