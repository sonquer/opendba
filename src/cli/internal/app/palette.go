package app

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/sonquer/tui4db/src/cli/internal/export"
	"github.com/sonquer/tui4db/src/cli/internal/ui"
)

type gotoMsg struct{ view view }

type reloadMsg struct{}

type newConnectionMsg struct{}

type quitMsg struct{}

type removeMsg struct{ name string }

type command struct {
	title string
	hint  string
	msg   tea.Msg
}

type palette struct {
	theme  *ui.Theme
	filter textinput.Model
	items  []command
	cursor int
}

const (
	paletteWidth   = 54
	minPaletteRows = 3
)

func newPalette(theme *ui.Theme, keys keymap) palette {
	filter := input(theme, "", false)
	return palette{
		theme:  theme,
		filter: filter,
		items: []command{
			{title: "dashboard", hint: keys.Home.Help().Key, msg: gotoMsg{view: viewDashboard}},
			{title: "ask", hint: keys.Ask.Help().Key, msg: gotoMsg{view: viewAsk}},
			{title: "query editor", hint: keys.Query.Help().Key, msg: gotoMsg{view: viewQuery}},
			{title: "explain the statement", hint: keys.Explain.Help().Key, msg: explainMsg{}},
			{title: "history", hint: keys.History.Help().Key, msg: gotoMsg{view: viewHistory}},
			{title: "export the result", hint: keys.Export.Help().Key, msg: exportMsg{}},
			{title: "mouse on or off", hint: "off lets the terminal select text", msg: mouseMsg{}},
			{title: "copy the result as csv", hint: keys.CopyRow.Help().Key,
				msg: copyMsg{whole: true, format: export.FormatCSV}},
			{title: "copy the result as json", hint: "the whole result",
				msg: copyMsg{whole: true, format: export.FormatJSON}},
			{title: "copy the result as markdown", hint: "the whole result",
				msg: copyMsg{whole: true, format: export.FormatMarkdown}},
			{title: "new tab", hint: keys.NewTab.Help().Key, msg: newSheetMsg{}},
			{title: "close this tab", hint: keys.CloseTab.Help().Key, msg: askCloseMsg{}},
			{title: "next tab", hint: keys.NextTab.Help().Key, msg: walkSheetMsg{step: 1}},
			{title: "previous tab", hint: keys.PrevTab.Help().Key, msg: walkSheetMsg{step: -1}},
			{title: "tables", hint: keys.Schema.Help().Key, msg: gotoMsg{view: viewSchema}},
			{title: "indexes", hint: keys.Indexes.Help().Key, msg: gotoMsg{view: viewIndexes}},
			{title: "reload everything", hint: keys.Reload.Help().Key, msg: reloadMsg{}},
			{title: "database and schemas", hint: keys.Catalog.Help().Key, msg: gotoMsg{view: viewCatalog}},
			{title: "connections", hint: keys.Connections.Help().Key, msg: gotoMsg{view: viewSwitch}},
			{title: "new connection", hint: keys.New.Help().Key, msg: newConnectionMsg{}},
			{title: "ai settings", hint: "instances and models", msg: gotoMsg{view: viewAI}},
			{title: "release the model", hint: "gives back the memory it holds", msg: releaseMsg{}},
			{title: "keys and safety", hint: keys.Help.Help().Key, msg: gotoMsg{view: viewHelp}},
			{title: "quit", hint: keys.Quit.Help().Key, msg: quitMsg{}},
		},
	}
}

// withTabs adds the tabs to the command list. They are commands rather than
// only keys because the tabs of the editor are the one part of this program
// where what is on screen is a list that grows, and a list that grows is a list
// worth being able to search rather than count along.
func (p palette) withTabs(tabs []command) palette {
	p.items = append(append([]command{}, p.items...), tabs...)
	return p
}

func (p palette) matches() []command {
	needle := strings.ToLower(strings.TrimSpace(p.filter.Value()))
	if needle == "" {
		return p.items
	}
	found := make([]command, 0, len(p.items))
	for _, item := range p.items {
		if strings.Contains(strings.ToLower(item.title), needle) {
			found = append(found, item)
		}
	}
	return found
}

func (p palette) move(step int) palette {
	total := len(p.matches())
	if total == 0 {
		p.cursor = 0
		return p
	}
	p.cursor = (p.cursor + step + total) % total
	return p
}

func (p palette) selected() (command, bool) {
	found := p.matches()
	if p.cursor < 0 || p.cursor >= len(found) {
		return command{}, false
	}
	return found[p.cursor], true
}

func (p palette) edit(msg tea.KeyPressMsg) (palette, tea.Cmd) {
	updated, cmd := p.filter.Update(msg)
	p.filter = updated
	if p.cursor >= len(p.matches()) {
		p.cursor = 0
	}
	return p, cmd
}

func (p palette) view(width, height int) string {
	inner := paletteInner(width)
	rows := paletteRows(height)
	filter := p.filter
	filter.SetWidth(inner - 4)
	lines := []string{
		p.theme.Prompt.Render("› ") + filter.View(),
		p.theme.Rule(inner),
	}
	found := p.matches()
	if len(found) == 0 {
		lines = append(lines, p.theme.Muted.Render("  nothing matches"))
	}
	list := newPicker(p.theme, "")
	shown := found
	if len(shown) > rows {
		shown = shown[:rows]
	}
	items := make([]row, 0, len(shown))
	for _, item := range shown {
		items = append(items, row{key: item.title, label: item.title, note: item.hint})
	}
	list = list.withRows(items)
	list.cursor = p.cursor
	if len(items) > 0 {
		lines = append(lines, list.view(inner))
	}
	if len(found) > rows {
		lines = append(lines, p.theme.Subtle.Render(
			fmt.Sprintf("  … %d more", len(found)-rows)))
	}
	return p.theme.Panel.Render(strings.Join(lines, "\n"))
}

func paletteRows(height int) int {
	rows := ui.BodyHeight(height) - 5
	if rows < minPaletteRows {
		return minPaletteRows
	}
	return rows
}

func paletteInner(width int) int {
	if limit := ui.TextWidth(width) - 6; limit < paletteWidth {
		return limit
	}
	return paletteWidth
}
