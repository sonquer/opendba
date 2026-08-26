package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/x/ansi"

	"charm.land/glamour/v2"
	glamourstyle "charm.land/glamour/v2/ansi"
)

// bare is the text of a styled line, which is what decides whether a line is
// empty once a renderer has padded it.
func bare(line string) string { return ansi.Strip(line) }

const (
	minMarkdownWidth = 40
	codeTheme        = "nord"
)

// Markdown renders documents the program writes: the help page, the details of a
// table, the statement that produced a result.
type Markdown struct {
	theme    *Theme
	renderer *glamour.TermRenderer
	width    int
	rendered map[string]string
}

// Markdown returns a renderer for the given width, reusing the current one when
// the width has not changed.
func (t *Theme) Markdown(width int) *Markdown {
	if width < minMarkdownWidth {
		width = minMarkdownWidth
	}
	if t.markdown != nil && t.markdown.width == width {
		return t.markdown
	}
	t.markdown = newMarkdown(t, width)
	return t.markdown
}

func newMarkdown(theme *Theme, width int) *Markdown {
	renderer, err := glamour.NewTermRenderer(
		glamour.WithStyles(theme.markdownStyle()),
		glamour.WithWordWrap(width),
		glamour.WithPreservedNewLines(),
	)
	if err != nil {
		return &Markdown{theme: theme, width: width, rendered: map[string]string{}}
	}
	return &Markdown{theme: theme, renderer: renderer, width: width, rendered: map[string]string{}}
}

// Render turns a markdown document into styled text. A document that cannot be
// rendered is shown as it was written, which is still readable.
func (m *Markdown) Render(document string) string {
	if cached, ok := m.rendered[document]; ok {
		return cached
	}
	out := m.theme.Base.Render(strings.TrimRight(document, "\n"))
	if m.renderer != nil {
		if styled, err := m.renderer.Render(document); err == nil {
			out = strings.Trim(styled, "\n")
		}
	}
	m.rendered[document] = out
	return out
}

// SQL renders a statement the way a code block in a document would be rendered.
func (m *Markdown) SQL(statement string) string {
	statement = strings.TrimRight(strings.Trim(untab(statement), "\n"), " ")
	if strings.TrimSpace(statement) == "" {
		return ""
	}
	return m.Render("```sql\n" + statement + "\n```")
}

const tabStop = "    "

func untab(text string) string { return strings.ReplaceAll(text, "\t", tabStop) }

// Statement draws SQL the way the editor draws it: highlighted, with the line
// numbers a person points at when they talk about a query.
func (t *Theme) Statement(sql string, width int) string {
	sql = strings.TrimSpace(untab(sql))
	if sql == "" {
		return t.Muted.Render("the server did not show this statement")
	}
	const gutter = 5
	markdown := t.Markdown(width - gutter)
	lines := []string{}
	for i, source := range strings.Split(sql, "\n") {
		number := t.Subtle.Render(pad(fmt.Sprintf("%3d", i+1), gutter))
		wrapped := blank(strings.Split(markdown.SQL(source), "\n"))
		if len(wrapped) == 0 {
			lines = append(lines, number)
			continue
		}
		for j, line := range wrapped {
			prefix := number
			if j > 0 {
				prefix = strings.Repeat(" ", gutter)
			}
			lines = append(lines, prefix+truncate(line, width-gutter))
		}
	}
	return strings.Join(lines, "\n")
}

// blank drops the empty rows a rendered code block is padded with, so the first
// line of the statement is line one.
func blank(lines []string) []string {
	for len(lines) > 0 && strings.TrimSpace(bare(lines[0])) == "" {
		lines = lines[1:]
	}
	for len(lines) > 0 && strings.TrimSpace(bare(lines[len(lines)-1])) == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

func (t *Theme) markdownStyle() glamourstyle.StyleConfig {
	text := hex(t.P.Fg)
	muted := hex(t.P.Muted)
	accent := hex(t.P.Accent)
	info := hex(t.P.Info)
	warn := hex(t.P.Warn)
	yes := true

	block := func(color *string, bold bool) glamourstyle.StyleBlock {
		primitive := glamourstyle.StylePrimitive{Color: color}
		if bold {
			primitive.Bold = &yes
		}
		return glamourstyle.StyleBlock{StylePrimitive: primitive}
	}

	return glamourstyle.StyleConfig{
		Document:       glamourstyle.StyleBlock{StylePrimitive: glamourstyle.StylePrimitive{Color: &text}, Margin: margin(0)},
		Paragraph:      block(&text, false),
		BlockQuote:     glamourstyle.StyleBlock{StylePrimitive: glamourstyle.StylePrimitive{Color: &muted, Italic: &yes}, Indent: indent(1)},
		Heading:        block(&info, true),
		H1:             glamourstyle.StyleBlock{StylePrimitive: glamourstyle.StylePrimitive{Color: &info, Bold: &yes, Upper: &yes}},
		H2:             glamourstyle.StyleBlock{StylePrimitive: glamourstyle.StylePrimitive{Color: &info, Bold: &yes, Upper: &yes}},
		H3:             block(&accent, true),
		H4:             block(&accent, false),
		H5:             block(&muted, false),
		H6:             block(&muted, false),
		Text:           glamourstyle.StylePrimitive{Color: &text},
		Strong:         glamourstyle.StylePrimitive{Color: &text, Bold: &yes},
		Emph:           glamourstyle.StylePrimitive{Color: &muted, Italic: &yes},
		Strikethrough:  glamourstyle.StylePrimitive{Color: &muted, CrossedOut: &yes},
		HorizontalRule: glamourstyle.StylePrimitive{Color: &muted, Format: "\n─────\n"},
		Item:           glamourstyle.StylePrimitive{Color: &text},
		Enumeration:    glamourstyle.StylePrimitive{Color: &muted, BlockPrefix: ". "},
		Task:           glamourstyle.StyleTask{Ticked: "✓ ", Unticked: "○ "},
		Link:           glamourstyle.StylePrimitive{Color: &info, Underline: &yes},
		LinkText:       glamourstyle.StylePrimitive{Color: &accent},
		Image:          glamourstyle.StylePrimitive{Color: &info, Underline: &yes},
		ImageText:      glamourstyle.StylePrimitive{Color: &muted, Format: "{{.text}}"},
		Code:           glamourstyle.StyleBlock{StylePrimitive: glamourstyle.StylePrimitive{Color: &accent}},
		CodeBlock: glamourstyle.StyleCodeBlock{
			StyleBlock: glamourstyle.StyleBlock{StylePrimitive: glamourstyle.StylePrimitive{Color: &text}, Margin: margin(0)},
			Theme:      codeTheme,
		},
		Table: glamourstyle.StyleTable{
			StyleBlock:      glamourstyle.StyleBlock{StylePrimitive: glamourstyle.StylePrimitive{Color: &text}},
			CenterSeparator: separator("┼"),
			ColumnSeparator: separator("│"),
			RowSeparator:    separator("─"),
		},
		DefinitionTerm:        glamourstyle.StylePrimitive{Color: &accent},
		DefinitionDescription: glamourstyle.StylePrimitive{Color: &text, BlockPrefix: "\n"},
		HTMLBlock:             block(&warn, false),
		HTMLSpan:              block(&warn, false),
	}
}

func separator(s string) *string { return &s }

func margin(value uint) *uint { return &value }

func indent(value uint) *uint { return &value }
