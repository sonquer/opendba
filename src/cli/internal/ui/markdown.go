package ui

import (
	"strings"

	"charm.land/glamour/v2"
	"charm.land/glamour/v2/ansi"
)

const (
	minMarkdownWidth = 40
	codeTheme        = "nord"
)

// Markdown renders documents the program writes: the help page, the details of
// a table, the statement that produced a result.
//
// A renderer is bound to the width it was built with, because word wrapping is
// fixed when Glamour builds it, and parsing markdown on every frame is far too
// slow for a view function. Both facts are handled here: one renderer per
// width, and a cache of what has already been rendered.
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
	statement = strings.TrimSpace(statement)
	if statement == "" {
		return ""
	}
	return m.Render("```sql\n" + statement + "\n```")
}

func (t *Theme) markdownStyle() ansi.StyleConfig {
	text := hex(t.P.Fg)
	muted := hex(t.P.Muted)
	accent := hex(t.P.Accent)
	info := hex(t.P.Info)
	warn := hex(t.P.Warn)
	yes := true

	block := func(color *string, bold bool) ansi.StyleBlock {
		primitive := ansi.StylePrimitive{Color: color}
		if bold {
			primitive.Bold = &yes
		}
		return ansi.StyleBlock{StylePrimitive: primitive}
	}

	return ansi.StyleConfig{
		Document:       ansi.StyleBlock{StylePrimitive: ansi.StylePrimitive{Color: &text}, Margin: margin(0)},
		Paragraph:      block(&text, false),
		BlockQuote:     ansi.StyleBlock{StylePrimitive: ansi.StylePrimitive{Color: &muted, Italic: &yes}, Indent: indent(1)},
		Heading:        block(&info, true),
		H1:             ansi.StyleBlock{StylePrimitive: ansi.StylePrimitive{Color: &info, Bold: &yes, Upper: &yes}},
		H2:             ansi.StyleBlock{StylePrimitive: ansi.StylePrimitive{Color: &info, Bold: &yes, Upper: &yes}},
		H3:             block(&accent, true),
		H4:             block(&accent, false),
		H5:             block(&muted, false),
		H6:             block(&muted, false),
		Text:           ansi.StylePrimitive{Color: &text},
		Strong:         ansi.StylePrimitive{Color: &text, Bold: &yes},
		Emph:           ansi.StylePrimitive{Color: &muted, Italic: &yes},
		Strikethrough:  ansi.StylePrimitive{Color: &muted, CrossedOut: &yes},
		HorizontalRule: ansi.StylePrimitive{Color: &muted, Format: "\n─────\n"},
		Item:           ansi.StylePrimitive{Color: &text},
		Enumeration:    ansi.StylePrimitive{Color: &muted, BlockPrefix: ". "},
		Task:           ansi.StyleTask{Ticked: "✓ ", Unticked: "○ "},
		Link:           ansi.StylePrimitive{Color: &info, Underline: &yes},
		LinkText:       ansi.StylePrimitive{Color: &accent},
		Image:          ansi.StylePrimitive{Color: &info, Underline: &yes},
		ImageText:      ansi.StylePrimitive{Color: &muted, Format: "{{.text}}"},
		Code:           ansi.StyleBlock{StylePrimitive: ansi.StylePrimitive{Color: &accent}},
		CodeBlock: ansi.StyleCodeBlock{
			StyleBlock: ansi.StyleBlock{StylePrimitive: ansi.StylePrimitive{Color: &text}, Margin: margin(0)},
			Theme:      codeTheme,
		},
		Table: ansi.StyleTable{
			StyleBlock:      ansi.StyleBlock{StylePrimitive: ansi.StylePrimitive{Color: &text}},
			CenterSeparator: separator("┼"),
			ColumnSeparator: separator("│"),
			RowSeparator:    separator("─"),
		},
		DefinitionTerm:        ansi.StylePrimitive{Color: &accent},
		DefinitionDescription: ansi.StylePrimitive{Color: &text, BlockPrefix: "\n"},
		HTMLBlock:             block(&warn, false),
		HTMLSpan:              block(&warn, false),
	}
}

func separator(s string) *string { return &s }

func margin(value uint) *uint { return &value }

func indent(value uint) *uint { return &value }
