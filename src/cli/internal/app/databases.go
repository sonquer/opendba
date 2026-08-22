package app

import (
	"context"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/sonquer/tui4db/src/cli/internal/driver"
	"github.com/sonquer/tui4db/src/cli/internal/ui"
)

const catalogTimeout = 15 * time.Second

type catalogMsg struct {
	databases []driver.Database
	schemas   []driver.Schema
	err       error
}

type entryKind string

const (
	entryDatabase entryKind = "database"
	entrySchema   entryKind = "schema"
)

type entry struct {
	kind    entryKind
	name    string
	note    string
	current bool
}

type catalog struct {
	theme   *ui.Theme
	entries []entry
	cursor  int
	failure string
}

func newCatalog(theme *ui.Theme) catalog { return catalog{theme: theme} }

func (c catalog) withCatalog(msg catalogMsg, schema string) catalog {
	c.entries = nil
	c.failure = ""
	if msg.err != nil {
		c.failure = msg.err.Error()
		return c
	}
	for _, database := range msg.databases {
		c.entries = append(c.entries, entry{
			kind:    entryDatabase,
			name:    database.Name,
			note:    database.Comment,
			current: database.Current,
		})
	}
	for _, found := range msg.schemas {
		if found.System {
			continue
		}
		c.entries = append(c.entries, entry{
			kind:    entrySchema,
			name:    found.Name,
			note:    ui.Plural(found.Tables, "table", "tables"),
			current: found.Name == schema,
		})
	}
	if c.cursor >= len(c.entries) {
		c.cursor = max(0, len(c.entries)-1)
	}
	return c
}

func (c catalog) move(step int) catalog {
	if len(c.entries) == 0 {
		return c
	}
	c.cursor = (c.cursor + step + len(c.entries)) % len(c.entries)
	return c
}

func (c catalog) selected() (entry, bool) {
	if c.cursor < 0 || c.cursor >= len(c.entries) {
		return entry{}, false
	}
	return c.entries[c.cursor], true
}

func (c catalog) view(width int) string {
	if c.failure != "" {
		return c.theme.Error.Render("✗ " + c.failure)
	}
	if len(c.entries) == 0 {
		return c.theme.Muted.Render("  reading the server")
	}
	lines := make([]string, 0, len(c.entries)+4)
	kind := entryKind("")
	for i, item := range c.entries {
		if item.kind != kind {
			kind = item.kind
			if len(lines) > 0 {
				lines = append(lines, "")
			}
			lines = append(lines, c.theme.Section(string(kind)+"s", "", width), "")
		}
		lines = append(lines, c.row(item, width, i == c.cursor))
	}
	return strings.Join(lines, "\n")
}

func (c catalog) row(item entry, width int, active bool) string {
	marker := "  "
	name := c.theme.Value.Render(item.name)
	if item.current {
		name = c.theme.Accent.Render(item.name + " ·")
	}
	if active {
		marker = c.theme.Accent.Render("▌ ")
	}
	if item.note == "" {
		return marker + name
	}
	return ui.SplitLine(marker+name, c.theme.Muted.Render(ui.Truncate(item.note, width/2)), width)
}

func (m Model) browseCatalog() (tea.Model, tea.Cmd) {
	m.view = viewCatalog
	m.offset = 0
	m.catalog = m.catalog.withCatalog(catalogMsg{}, "")
	m.editor.Blur()
	return m, m.readCatalog()
}

func (m Model) readCatalog() tea.Cmd {
	conn := m.session.Conn
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), catalogTimeout)
		defer cancel()

		databases, err := conn.Databases(ctx)
		if err != nil {
			return catalogMsg{err: err}
		}
		schemas, err := conn.Schemas(ctx)
		if err != nil {
			return catalogMsg{err: err}
		}
		return catalogMsg{databases: databases, schemas: schemas}
	}
}

func (m Model) catalogKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.Quit):
		m.quitting = true
		return m, tea.Quit
	case key.Matches(msg, m.keys.Back), key.Matches(msg, m.keys.Catalog):
		m.view = viewDashboard
	case key.Matches(msg, m.keys.Up):
		m.catalog = m.catalog.move(-1)
	case key.Matches(msg, m.keys.Down):
		m.catalog = m.catalog.move(1)
	case key.Matches(msg, m.keys.Choose):
		return m.enter()
	}
	return m, nil
}

func (m Model) enter() (tea.Model, tea.Cmd) {
	chosen, ok := m.catalog.selected()
	if !ok || chosen.current {
		m.view = viewDashboard
		return m, nil
	}
	m.view = viewDashboard
	m.loading = true
	m.failure = ""
	if chosen.kind == entrySchema {
		m.session.Connection.DefaultSchema = chosen.name
		return m, tea.Batch(m.load(), m.spinner.Tick, m.notify("reading "+chosen.name))
	}
	return m, tea.Batch(m.openDatabase(chosen.name), m.spinner.Tick)
}

func (m Model) openDatabase(database string) tea.Cmd {
	workspace := m.workspace
	name := m.session.Connection.Name
	return func() tea.Msg {
		session, cleanup, err := workspace.OpenDatabase(context.Background(), name, database)
		return switchedMsg{session: session, cleanup: cleanup, err: err}
	}
}
