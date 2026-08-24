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

// command is one thing the list can do: what it is called, the key it answers
// to, and the message it sends.
type command struct {
	title string
	key   string
	msg   tea.Msg

	// where is the screens this command belongs to.
	where []view
}

// here reports whether a command belongs on the screen in front.
func (c command) here(current view) bool {
	if len(c.where) == 0 {
		return true
	}
	for _, allowed := range c.where {
		if allowed == current {
			return true
		}
	}
	return false
}

type palette struct {
	theme  *ui.Theme
	filter textinput.Model
	items  []command
	cursor int
}

const (
	paletteWidth   = 64
	minPaletteRows = 3
)

// editing and asking are the screens whose commands are about what is on them
// rather than about the program.
var (
	onEditor = []view{viewQuery}
	onAsk    = []view{viewAsk}
)

func newPalette(theme *ui.Theme, keys keymap, current view) palette {
	filter := input(theme, "", false)
	offered := make([]command, 0, 32)
	for _, item := range every4Palette(keys) {
		if item.here(current) {
			offered = append(offered, item)
		}
	}
	return palette{theme: theme, filter: filter, items: offered}
}

func every4Palette(keys keymap) []command {
	return []command{
		{title: "dashboard", key: keys.Home.Help().Key, msg: gotoMsg{view: viewDashboard}},
		{title: "ask", key: keys.Ask.Help().Key, msg: gotoMsg{view: viewAsk}},
		{title: "query editor", key: keys.Query.Help().Key, msg: gotoMsg{view: viewQuery}},
		{title: "tables", key: keys.Schema.Help().Key, msg: gotoMsg{view: viewSchema}},
		{title: "indexes", key: keys.Indexes.Help().Key, msg: gotoMsg{view: viewIndexes}},
		{title: "history", key: keys.History.Help().Key, msg: gotoMsg{view: viewHistory}},
		{title: "database and schemas", key: keys.Catalog.Help().Key,
			msg: gotoMsg{view: viewCatalog}},
		{title: "connections", key: keys.Connections.Help().Key, msg: gotoMsg{view: viewSwitch}},
		{title: "new connection", key: keys.New.Help().Key, msg: newConnectionMsg{}},

		{title: "run the statement", key: keys.Run.Help().Key, where: onEditor,
			msg: runNowMsg{}},
		{title: "explain the statement", key: keys.Explain.Help().Key, where: onEditor,
			msg: explainMsg{}},
		{title: "export the result", key: keys.Export.Help().Key, where: onEditor,
			msg: exportMsg{}},
		{title: "save the tab to a file", key: keys.Write.Help().Key, where: onEditor,
			msg: saveFileMsg{}},
		{title: "copy the result as csv", key: keys.CopyRow.Help().Key, where: onEditor,
			msg: copyMsg{whole: true, format: export.FormatCSV}},
		{title: "copy the result as json", where: onEditor,
			msg: copyMsg{whole: true, format: export.FormatJSON}},
		{title: "copy the result as markdown", where: onEditor,
			msg: copyMsg{whole: true, format: export.FormatMarkdown}},
		{title: "new tab", key: keys.NewTab.Help().Key, where: onEditor, msg: newSheetMsg{}},
		{title: "close this tab", key: keys.CloseTab.Help().Key, where: onEditor,
			msg: askCloseMsg{}},
		{title: "next tab", key: keys.NextTab.Help().Key, where: onEditor,
			msg: walkSheetMsg{step: 1}},
		{title: "previous tab", key: keys.PrevTab.Help().Key, where: onEditor,
			msg: walkSheetMsg{step: -1}},

		{title: "conversations", key: keys.History.Help().Key, where: onAsk,
			msg: openChatsMsg{}},
		{title: "new conversation", key: keys.NewTab.Help().Key, where: onAsk,
			msg: newChatMsg{}},

		{title: "reload everything", key: keys.Reload.Help().Key, msg: reloadMsg{}},
		{title: "settings", msg: preferencesMsg{}},
		{title: "assistant and models", msg: gotoMsg{view: viewAI}},
		{title: "release the model", msg: releaseMsg{}},
		{title: "mouse on or off", msg: mouseMsg{}},
		{title: "keys and safety", key: keys.Help.Help().Key, msg: gotoMsg{view: viewHelp}},
		{title: "quit", key: keys.Quit.Help().Key, msg: quitMsg{}},
	}
}

// withTabs adds the tabs to the command list.
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
		items = append(items, row{key: item.title, label: item.title, cap: item.key})
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
