package app

import (
	"context"
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

const (
	sectionDatabases = "databases"
	sectionSchemas   = "schemas"
)

type catalog struct {
	picker
	failure string
}

func newCatalog(theme *ui.Theme) catalog {
	return catalog{picker: newPicker(theme, "reading the server")}
}

func (c catalog) withCatalog(msg catalogMsg, schema string) catalog {
	c.failure = ""
	if msg.err != nil {
		c.failure = msg.err.Error()
		c.picker = c.withRows(nil)
		return c
	}
	rows := make([]row, 0, len(msg.databases)+len(msg.schemas))
	for _, database := range msg.databases {
		rows = append(rows, row{
			key:     sectionDatabases + ":" + database.Name,
			label:   database.Name,
			note:    database.Comment,
			section: sectionDatabases,
			current: database.Current,
		})
	}
	for _, found := range msg.schemas {
		if found.System {
			continue
		}
		rows = append(rows, row{
			key:     sectionSchemas + ":" + found.Name,
			label:   found.Name,
			note:    ui.Plural(found.Tables, "table", "tables"),
			section: sectionSchemas,
			current: found.Name == schema,
		})
	}
	c.picker = c.withRows(rows)
	return c
}

func (c catalog) view(width int) string {
	if c.failure != "" {
		return c.theme.Error.Render("✗ " + c.failure)
	}
	return c.picker.view(width)
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
		return m.confirmQuit()
	case key.Matches(msg, m.keys.Back), key.Matches(msg, m.keys.Catalog):
		m.view = viewDashboard
	case key.Matches(msg, m.keys.Up):
		m.catalog.picker = m.catalog.move(-1)
	case key.Matches(msg, m.keys.Down):
		m.catalog.picker = m.catalog.move(1)
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
	if chosen.section == sectionSchemas {
		m.session.Connection.DefaultSchema = chosen.label
		return m, tea.Batch(m.load(), m.spinner.Tick,
			m.remember(m.session.Connection.Database, chosen.label),
			m.notify("reading "+chosen.label))
	}
	return m, tea.Batch(m.openDatabase(chosen.label), m.spinner.Tick)
}

// remember writes the place you moved to back to the profile, so the next run
// opens where you left off. Failing to write it is worth saying and nothing
// more: the session is already there.
func (m Model) remember(database, schema string) tea.Cmd {
	workspace := m.workspace
	name := m.session.Connection.Name
	return func() tea.Msg {
		if err := workspace.Remember(name, database, schema); err != nil {
			return rememberedMsg{err: err}
		}
		return rememberedMsg{}
	}
}

type rememberedMsg struct{ err error }

func (m Model) openDatabase(database string) tea.Cmd {
	workspace := m.workspace
	name := m.session.Connection.Name
	return func() tea.Msg {
		session, cleanup, err := workspace.OpenDatabase(context.Background(), name, database)
		return switchedMsg{session: session, cleanup: cleanup, err: err}
	}
}
