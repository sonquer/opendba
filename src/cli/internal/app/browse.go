package app

import (
	"sort"
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/sonquer/opendba/src/cli/internal/driver"
	"github.com/sonquer/opendba/src/cli/internal/ui"
)

// browse is what has been done to one of the two catalogue lists: what it has
// been narrowed to, and what it has been put in the order of.
type browse struct {
	filter   textinput.Model
	typing   bool
	column   int
	reversed bool
}

// columns is how many ways a catalogue list can be sorted: the name, the three
// facts, and the bar.
const columns = 5

func newBrowse(theme *ui.Theme, column int, reversed bool) browse {
	return browse{filter: input(theme, "", false), column: column, reversed: reversed}
}

// needle is what was typed, ready to be matched against a name.
func (b browse) needle() string {
	return strings.ToLower(strings.TrimSpace(b.filter.Value()))
}

func (b browse) active() bool { return b.needle() != "" }

// turn moves to the next column, wrapping, and starts each new column in the
// order that column is usually wanted in: a name from the top, a number from the
// largest.
func (b browse) turn() browse {
	b.column = (b.column + 1) % columns
	b.reversed = b.column != 0
	return b
}

func (b browse) clear() browse {
	b.filter.SetValue("")
	b.typing = false
	return b
}

// tag says a list is hiding rows, because a list that is hiding rows and does
// not say so is a list that is missing rows.
func (b browse) tag(shown, total int) string {
	if !b.active() {
		return ""
	}
	return ui.Count(int64(shown)) + " of " + ui.Count(int64(total)) + " match " + b.needle()
}

func (b browse) view(theme *ui.Theme, width int) string {
	if !b.typing && !b.active() {
		return ""
	}
	b.filter.SetWidth(width - 4)
	line := theme.Prompt.Render("› ") + b.filter.View()
	if !b.typing {
		line = theme.Prompt.Render("› ") + theme.Value.Render(b.filter.Value())
	}
	return line + "\n"
}

// which of the two lists this screen is. The pair is an array rather than a map
// so that a copy of the model is a copy of both.
func (m Model) which() int {
	if m.view == viewIndexes {
		return 1
	}
	return 0
}

// shownTables is the list the tables screen draws, and the list everything that
// indexes a row must index: the cursor, the page enter opens, and the count in
// the header all read the same slice.
func (m Model) shownTables() []driver.Table {
	at := m.lists[m.which()]
	kept := make([]driver.Table, 0, len(m.tables))
	for _, table := range m.tables {
		if matchesNeedle(table.Qualified(), at.needle()) {
			kept = append(kept, table)
		}
	}
	return sortTables(kept, at)
}

func (m Model) shownIndexes() []driver.Index {
	at := m.lists[m.which()]
	kept := make([]driver.Index, 0, len(m.indexes))
	for _, index := range m.indexes {
		if matchesNeedle(index.Name, at.needle()) || matchesNeedle(index.Table, at.needle()) {
			kept = append(kept, index)
		}
	}
	return sortIndexes(kept, at)
}

func matchesNeedle(name, needle string) bool {
	return needle == "" || strings.Contains(strings.ToLower(name), needle)
}

func sortTables(tables []driver.Table, at browse) []driver.Table {
	less := []func(a, b driver.Table) bool{
		func(a, b driver.Table) bool { return a.Qualified() < b.Qualified() },
		func(a, b driver.Table) bool { return a.Rows < b.Rows },
		func(a, b driver.Table) bool { return a.Size < b.Size },
		func(a, b driver.Table) bool { return a.IndexSize < b.IndexSize },
		func(a, b driver.Table) bool { return a.CacheHit < b.CacheHit },
	}
	sort.SliceStable(tables, ordering(at, func(i, j int) bool {
		return less[at.column](tables[i], tables[j])
	}))
	return tables
}

func sortIndexes(indexes []driver.Index, at browse) []driver.Index {
	less := []func(a, b driver.Index) bool{
		func(a, b driver.Index) bool { return a.Name < b.Name },
		func(a, b driver.Index) bool { return a.Table < b.Table },
		func(a, b driver.Index) bool { return a.Size < b.Size },
		func(a, b driver.Index) bool { return a.Scans < b.Scans },
		func(a, b driver.Index) bool { return a.Scans < b.Scans },
	}
	sort.SliceStable(indexes, ordering(at, func(i, j int) bool {
		return less[at.column](indexes[i], indexes[j])
	}))
	return indexes
}

// ordering turns a comparison into the one sort.SliceStable wants, the right
// way round.
func ordering(at browse, less func(i, j int) bool) func(i, j int) bool {
	if at.reversed {
		return func(i, j int) bool { return less(j, i) }
	}
	return less
}

// browseKey answers the two catalogue screens.
func (m Model) browseKey(msg tea.KeyPressMsg) (Model, tea.Cmd, bool) {
	at := m.which()
	if m.lists[at].typing {
		return m.filterKey(msg)
	}
	switch {
	case key.Matches(msg, m.keys.Find):
		m.lists[at].typing = true
		m.listing = 0
		return m, m.lists[at].filter.Focus(), true
	case key.Matches(msg, m.keys.Order):
		m.lists[at] = m.lists[at].turn()
		m.listing = 0
		return m, nil, true
	case key.Matches(msg, m.keys.Reverse):
		m.lists[at].reversed = !m.lists[at].reversed
		m.listing = 0
		return m, nil, true
	}
	return m, nil, false
}

func (m Model) filterKey(msg tea.KeyPressMsg) (Model, tea.Cmd, bool) {
	at := m.which()
	switch {
	case key.Matches(msg, m.keys.Back):
		m.lists[at] = m.lists[at].clear()
		m.listing = 0
		return m, nil, true
	case key.Matches(msg, m.keys.Choose):
		m.lists[at].typing = false
		m.lists[at].filter.Blur()
		return m, nil, true
	case msg.String() == "up", msg.String() == "down":
		return m.walkList(msg.String() == "down"), nil, true
	}
	updated, cmd := m.lists[at].filter.Update(msg)
	m.lists[at].filter = updated
	m.listing = 0
	return m, cmd, true
}

func (m Model) walkList(down bool) Model {
	step := -1
	if down {
		step = 1
	}
	moved, _ := m.walkListing(step)
	return moved
}
