package shot

import (
	"encoding/hex"
	"fmt"
	"image/color"
	"io"
	"strings"
)

const (
	cellWidth  = 8.4
	cellHeight = 17.0
	fontSize   = 14.0
	baseline   = 13.0
	padding    = 8.0
)

const fontStack = "ui-monospace, SFMono-Regular, Menlo, Consolas, 'DejaVu Sans Mono', monospace"

// Cell is one character on the screen, with the colours it was drawn in.
type Cell struct {
	Content   string
	Fg        color.Color
	Bg        color.Color
	Bold      bool
	Underline bool
}

// SVG draws a grid of cells as a picture, so that a screen from a run that has
// already ended can still be looked at in the colours it had.
func SVG(out io.Writer, grid [][]Cell, fg, bg color.Color) error {
	rows := len(grid)
	columns := 0
	for _, row := range grid {
		columns = max(columns, len(row))
	}
	width := float64(columns)*cellWidth + padding*2
	height := float64(rows)*cellHeight + padding*2

	var body strings.Builder
	fmt.Fprintf(&body,
		`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 %.1f %.1f" width="%.1f" height="%.1f" font-family="%s" font-size="%.1f">`,
		width, height, width, height, fontStack, fontSize)
	fmt.Fprintf(&body, `<rect width="100%%" height="100%%" fill="%s"/>`, paint(bg, "#000000"))
	for y, row := range grid {
		writeRow(&body, row, y, fg, bg)
	}
	body.WriteString("</svg>\n")
	if _, err := io.WriteString(out, body.String()); err != nil {
		return fmt.Errorf("write the picture: %w", err)
	}
	return nil
}

func writeRow(body *strings.Builder, row []Cell, y int, fg, bg color.Color) {
	top := padding + float64(y)*cellHeight
	for _, run := range runs(row) {
		if !same(run.cell.Bg, bg) {
			fmt.Fprintf(body, `<rect x="%.1f" y="%.1f" width="%.1f" height="%.1f" fill="%s"/>`,
				padding+float64(run.from)*cellWidth, top,
				float64(run.width())*cellWidth, cellHeight, paint(run.cell.Bg, paint(bg, "#000000")))
		}
	}
	for _, run := range runs(row) {
		if strings.TrimSpace(run.text) == "" {
			continue
		}
		weight := ""
		if run.cell.Bold {
			weight = ` font-weight="bold"`
		}
		decoration := ""
		if run.cell.Underline {
			decoration = ` text-decoration="underline"`
		}
		fmt.Fprintf(body, `<text x="%.1f" y="%.1f" fill="%s"%s%s xml:space="preserve">%s</text>`,
			padding+float64(run.from)*cellWidth, top+baseline,
			paint(run.cell.Fg, paint(fg, "#dcdcdc")), weight, decoration, escape(run.text))
	}
}

type run struct {
	from int
	to   int
	text string
	cell Cell
}

func (r run) width() int { return r.to - r.from }

func runs(row []Cell) []run {
	var out []run
	for i, cell := range row {
		if len(out) > 0 && joins(out[len(out)-1].cell, cell) {
			out[len(out)-1].to = i + 1
			out[len(out)-1].text += content(cell)
			continue
		}
		out = append(out, run{from: i, to: i + 1, text: content(cell), cell: cell})
	}
	return out
}

func content(cell Cell) string {
	if cell.Content == "" {
		return " "
	}
	return cell.Content
}

func joins(a, b Cell) bool {
	return a.Bold == b.Bold && a.Underline == b.Underline && same(a.Fg, b.Fg) && same(a.Bg, b.Bg)
}

func same(a, b color.Color) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	ar, ag, ab, aa := a.RGBA()
	br, bg, bb, ba := b.RGBA()
	return ar == br && ag == bg && ab == bb && aa == ba
}

// paint names a colour the way SVG names one, falling back when the terminal
// left it to the default.
func paint(value color.Color, fallback string) string {
	if value == nil {
		return fallback
	}
	r, g, b, _ := value.RGBA()
	out := []byte{byte(r >> 8), byte(g >> 8), byte(b >> 8)}
	return "#" + hex.EncodeToString(out)
}

var escaper = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;")

func escape(text string) string { return escaper.Replace(text) }
