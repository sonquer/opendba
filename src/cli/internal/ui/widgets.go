package ui

import (
	"image/color"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// ReadOnlyLabel is the mode every driver falls back to, and the only one the
// interface renders quietly.
const ReadOnlyLabel = "READ ONLY"

func (t *Theme) Rule(width int) string {
	if width <= 0 {
		return ""
	}
	return t.Separator.Render(strings.Repeat("─", width))
}

const (
	identityName   = 40
	identityServer = 48
)

// IdentityLine says where you are, in the shape of a shell prompt: a path, cut
// by slashes, the way a shell writes where it is. The colour of the environment
// is the line across the top, not a mark in the text.
func (t *Theme) IdentityLine(env EnvColor, name, server, mode string) string {
	line := t.Prompt.Render("→ ") + t.Muted.Render("~ ") + t.Title.Render(truncate(name, identityName))
	if server != "" {
		line += t.Separator.Render(pathSeparator) + t.Muted.Render(truncate(server, identityServer))
	}
	if mode != "" {
		line += " " + t.Mode(mode)
	}
	return line
}

// pathSeparator cuts the parts of the header apart. A chevron points from where
// you are to what is inside it, which is what the header is: a path, not a list
// and not a file name.
const pathSeparator = " › "

// Slashed joins the parts of a path, leaving out the ones a driver does not
// have.
func Slashed(parts ...string) string { return join(pathSeparator, parts) }

// Mode is the one word on the header that changes what this program will do, so
// it is a label rather than a remark.
func (t *Theme) Mode(mode string) string {
	if strings.EqualFold(mode, ReadOnlyLabel) {
		return t.BadgeStyle.Render(mode)
	}
	return t.BadgeStyle.Background(t.P.Warn).Render(mode)
}

// Headline is the one line someone who does not run databases for a living
// reads and stops at: a word for the state on a badge, what it is about, and
// how much came back fine. The badge carries the weight, because a glyph in
// front of a sentence is a sentence.
func (t *Theme) Headline(severity Severity, subject, balance string, width int) string {
	badge := t.BadgeStyle.Background(t.Severity(severity).GetForeground()).Render(verdictWord(severity))
	left := badge + "  " + t.Value.Render(subject)
	return SplitLine(left, t.Muted.Render(balance), width)
}

// verdictWord is the state in the language of what to do about it. It is the
// same judgement as Verdict, said loudly enough for a badge.
func verdictWord(severity Severity) string {
	switch severity {
	case SevCritical:
		return "ACT"
	case SevWarn:
		return "WATCH"
	case SevOK:
		return "OK"
	default:
		return "UNKNOWN"
	}
}

func (t *Theme) Section(title, tag string, width int) string {
	return SplitLine(t.SectionHead.Render(strings.ToUpper(title)), tag, width)
}

// Screen names a whole screen rather than a block inside one, so it is not
// drawn like the blocks: the accent, and a rule under the word itself, which is
// what separates a title from the headings underneath it.
func (t *Theme) Screen(title, tag string, width int) string {
	title = strings.ToUpper(title)
	return SplitLine(t.Accent.Bold(true).Render(title), tag, width) + "\n" +
		t.Accent.Render(strings.Repeat("▔", lipgloss.Width(title)))
}

// SplitLine pushes right to the far end of the given width.
func SplitLine(left, right string, width int) string {
	if right == "" {
		return left
	}
	room := width - lipgloss.Width(left)
	if room < lipgloss.Width(right)+2 {
		room = lipgloss.Width(right) + 2
	}
	return left + lipgloss.NewStyle().Width(room).AlignHorizontal(lipgloss.Right).Render(right)
}

const gaugeWidth = 20

// Gauge is a measurement drawn as a bar, filled to the ratio and coloured by
// what the reading means. What the bar is made of is a style, because how well
// a glyph draws is the font's business and fonts differ.
func (t *Theme) Gauge(ratio float64, sev Severity) string {
	return t.draw(ratio, sev, gaugeWidth)
}

// Blank is the space a gauge would take, so rows without a measurement still
// line up with the rows that have one.
func (t *Theme) Blank() string { return strings.Repeat(" ", t.BarWidth(gaugeWidth)) }

// Reading is one line of a health report: what was measured, where it sits, and
// what that means. The row shows the first two; the rest is behind enter.
type Reading struct {
	Code     string
	Severity Severity
	Label    string
	Value    string
	Note     string
	Ratio    float64
	Measured bool
	Cursor   bool
}

// Columns are the widths readings line up on. Measuring every reading on the
// screen once, rather than each block on its own, is what keeps separate groups
// in the same columns.
type Columns struct {
	Label int
	Value int
}

// Measure returns the columns a set of readings needs.
func Measure(readings []Reading) Columns {
	var at Columns
	for _, reading := range readings {
		at.Label = max(at.Label, lipgloss.Width(reading.Label))
		at.Value = max(at.Value, lipgloss.Width(reading.Value))
	}
	return at
}

// Readings draws a group as aligned rows: what it is, where it sits on its
// scale, the number, and one word for what that means. The sentence behind the
// word is on the page, which is what enter is for.
func (t *Theme) Readings(readings []Reading, width int, at Columns) string {
	labels, values := at.Label, at.Value
	if labels == 0 && values == 0 {
		measured := Measure(readings)
		labels, values = measured.Label, measured.Value
	}
	lines := make([]string, 0, len(readings))
	for _, reading := range readings {
		marker := "  "
		label := t.Value.Render(pad(reading.Label, labels))
		if reading.Cursor {
			marker = t.Accent.Render("▌ ")
			label = t.Accent.Render(pad(reading.Label, labels))
		}
		bar := t.Blank()
		if reading.Measured {
			bar = t.Gauge(reading.Ratio, reading.Severity)
		}
		lines = append(lines, truncate(marker+label+"   "+bar+"   "+
			t.Value.Render(padLeft(reading.Value, values))+"   "+
			t.Severity(reading.Severity).Render(Verdict(reading.Severity)), width))
	}
	return strings.Join(lines, "\n")
}

// Verdict is the one word a reading gets beside its number, in the language of
// what to do about it rather than of how the check is named.
func Verdict(severity Severity) string {
	switch severity {
	case SevOK:
		return "ok"
	case SevWarn:
		return "watch"
	case SevCritical:
		return "act"
	case SevInfo:
		return "note"
	default:
		return "n/a"
	}
}

// Meter is the gauge a reading gets when there is room to draw it properly: the
// whole width, with the ends of the scale named.
func (t *Theme) Meter(reading Reading, width int) string {
	if !reading.Measured {
		return t.Muted.Render("this reading is a count, not a proportion")
	}
	inner := max(width-2, gaugeWidth)
	return t.draw(reading.Ratio, reading.Severity, inner) + "\n" +
		SplitLine(t.Subtle.Render("0%"), t.Subtle.Render("100%"), width)
}

func (t *Theme) KV(label string, labelWidth int, value string) string {
	return t.Label.Render(pad(label, labelWidth)) + " " + t.Value.Render(value)
}

func Dotted(parts ...string) string { return join(" · ", parts) }

// Hint is one key and what pressing it does.
type Hint struct{ Key, Does string }

// Hints is the row of keys a dialog offers, each on the same grey cap the
// footer of every screen puts them on. A bare letter beside a word does not
// read as something to press, and a dialog has no footer to learn that from.
func (t *Theme) Hints(hints ...Hint) string {
	parts := make([]string, 0, len(hints))
	for _, hint := range hints {
		parts = append(parts, t.KeycapStyle.Render(Keystroke(hint.Key))+t.Muted.Render(" "+hint.Does))
	}
	return strings.Join(parts, t.Subtle.Render(" · "))
}

func join(separator string, parts []string) string {
	kept := parts[:0]
	for _, p := range parts {
		if strings.TrimSpace(p) != "" {
			kept = append(kept, p)
		}
	}
	return strings.Join(kept, separator)
}

// Checkbox and Radio are what a form uses to say what is chosen: a box for the
// choices that stack, a dot for the choice that replaces.
func Checkbox(on bool) string {
	if on {
		return "▣"
	}
	return "▢"
}

func Radio(on bool) string {
	if on {
		return "●"
	}
	return "○"
}

func (t *Theme) Badge(text string, sev Severity) string {
	return t.Severity(sev).Bold(true).Render(text)
}

// pad fills a cell to the given width, clipping what does not fit. Widths come
// from lipgloss so that wide runes and styled text are measured, not counted.
// padLeft pushes a value to the right of its column, which is where numbers
// belong when they are read down a list.
func padLeft(s string, w int) string {
	if w <= 0 {
		return ""
	}
	return lipgloss.NewStyle().Width(w).AlignHorizontal(lipgloss.Right).MaxHeight(1).
		Render(truncate(s, w))
}

func pad(s string, w int) string {
	if w <= 0 {
		return ""
	}
	return lipgloss.NewStyle().Width(w).MaxHeight(1).Render(truncate(s, w))
}

// truncate clips styled text without cutting an escape sequence in half.
func truncate(s string, w int) string {
	if w <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= w {
		return s
	}
	return ansi.Truncate(s, w, "…")
}

// Clip cuts a sentence at the last word that fits, because a sentence cut mid
// word reads as a bug rather than as a summary.
func Clip(text string, width int) string {
	if width <= 0 {
		return ""
	}
	if lipgloss.Width(text) <= width {
		return text
	}
	cut := truncate(text, width)
	if space := strings.LastIndex(cut, " "); space > width/2 {
		return strings.TrimRight(cut[:space], " ,;:") + "…"
	}
	return cut
}

// Truncate clips styled text to a width, marking what was left out.
func Truncate(s string, w int) string { return truncate(s, w) }

// Pad fills a cell to a width.
func Pad(s string, w int) string { return pad(s, w) }

// Listing is one row of a list of things a server holds: what it is called, the
// numbers worth reading down a column, the one number that is a proportion, and
// a word for what all of it means.
type Listing struct {
	Name     string
	Facts    []string
	Note     string
	Ratio    float64
	Measured bool
	Severity Severity
	Cursor   bool
}

// Head names the columns of a list, because a column of numbers with nothing
// over it is a column of numbers nobody can read.
type Head struct {
	Name  string
	Facts []string
	Gauge string
	Note  string
}

// Rows draws a list of them under their headings, every column measured across
// the whole list so the numbers line up and a long name cannot push one row out
// of step.
func (t *Theme) Rows(list []Listing, head Head, width int) string {
	shape := listingColumns(list, head, width)
	lines := make([]string, 0, len(list)+2)
	lines = append(lines, t.heading(head, shape, width), t.Rule(width))
	for _, row := range list {
		lines = append(lines, t.row(row, shape, width))
	}
	return strings.Join(lines, "\n")
}

// row draws one line of a list. The row the cursor is on is painted end to end
// rather than marked at its edge, which is how the list of sessions marks its
// own, and every part of it carries the background: a styled run ends with a
// reset, and a reset would end a background set around the outside.
func (t *Theme) row(item Listing, shape shape, width int) string {
	text, muted, verdict := t.Value, t.Muted, t.Severity(item.Severity)
	var ground color.Color
	if item.Cursor {
		ground = t.P.Selection
		text = text.Foreground(t.P.OnSelection).Background(ground)
		muted, verdict = muted.Background(ground), verdict.Background(ground)
	}
	line := text.Render("  " + pad(item.Name, shape.name))
	for i, fact := range item.Facts {
		line += text.Render("   " + padLeft(fact, shape.facts[i]))
	}
	if shape.bars {
		bar := text.Render(t.Blank())
		if item.Measured {
			bar = t.drawOn(item.Ratio, item.Severity, gaugeWidth, ground)
		}
		line += text.Render("   ") + bar
	}
	if shape.note > 0 {
		line += text.Render("  ") + muted.Render(pad(item.Note, shape.note))
	}
	line = truncate(line+text.Render("  ")+verdict.Render(Verdict(item.Severity)), width)
	if gap := width - lipgloss.Width(line); gap > 0 {
		line += text.Render(strings.Repeat(" ", gap))
	}
	return line
}

// heading draws the names of the columns into the same widths the rows use.
func (t *Theme) heading(head Head, shape shape, width int) string {
	line := "  " + pad(head.Name, shape.name)
	for i := range shape.facts {
		line += "   " + padLeft(at(head.Facts, i), shape.facts[i])
	}
	if shape.bars {
		line += "   " + pad(head.Gauge, gaugeWidth+2)
	}
	if shape.note > 0 {
		line += "  " + pad(head.Note, shape.note)
	}
	return t.TableHead.Render(truncate(line, width))
}

func at(values []string, i int) string {
	if i >= len(values) {
		return ""
	}
	return values[i]
}

// shape is what one list of rows fits into at one width.
type shape struct {
	name  int
	note  int
	facts []int
	bars  bool
}

// listingColumns measures what every row has to fit into. The numbers keep
// their width, because a number that is cut is wrong. What goes first on a
// narrow window is the sentence, then the bar, and the name gives way last.
func listingColumns(list []Listing, head Head, width int) shape {
	measured := shape{bars: true, name: lipgloss.Width(head.Name), note: lipgloss.Width(head.Note)}
	for i, fact := range head.Facts {
		if i >= len(measured.facts) {
			measured.facts = append(measured.facts, 0)
		}
		measured.facts[i] = lipgloss.Width(fact)
	}
	for _, row := range list {
		measured.name = max(measured.name, lipgloss.Width(row.Name))
		measured.note = max(measured.note, lipgloss.Width(row.Note))
		for i, fact := range row.Facts {
			if i >= len(measured.facts) {
				measured.facts = append(measured.facts, 0)
			}
			measured.facts[i] = min(max(measured.facts[i], lipgloss.Width(fact)), maxFact)
		}
	}
	spent := listingSlack
	for _, fact := range measured.facts {
		spent += fact + 3
	}
	if width-spent-measured.note-gaugeWidth-5 < roomForName {
		measured.note = 0
	}
	if width-spent-gaugeWidth-5 < roomForName {
		measured.bars = false
	}
	if measured.bars {
		spent += gaugeWidth + 5
	}
	measured.name = min(measured.name, max(width-spent-measured.note, minName))
	return measured
}

const (
	// listingSlack is the marker, the gap before the verdict, and the verdict.
	listingSlack = 11
	roomForName  = 20
	minName      = 12

	// maxFact keeps one long value in a column from starving every name in the
	// list of the room to be read.
	maxFact = 20
)
