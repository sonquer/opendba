package app

import (
	"context"
	"slices"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/sonquer/tui4db/src/cli/internal/config"
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

// catalog is a form rather than a menu: one database, any number of schemas,
// and nothing applied until it is saved.
type catalog struct {
	picker
	failure  string
	database string
	current  string
	schemas  map[string]bool
}

func newCatalog(theme *ui.Theme) catalog {
	return catalog{picker: newPicker(theme, "reading the server"), schemas: map[string]bool{}}
}

func (c catalog) withCatalog(msg catalogMsg, connection config.Connection) catalog {
	c.failure = ""
	if msg.err != nil {
		c.failure = msg.err.Error()
		c.picker = c.withRows(nil)
		return c
	}
	c.database, c.current, c.schemas = connection.Database, connection.Database, map[string]bool{}
	for _, schema := range connection.Filter() {
		c.schemas[schema] = true
	}
	rows := make([]row, 0, len(msg.databases)+len(msg.schemas))
	for _, database := range msg.databases {
		if database.Current {
			c.database, c.current = database.Name, database.Name
		}
		rows = append(rows, row{
			key:     sectionDatabases + ":" + database.Name,
			label:   database.Name,
			note:    ui.Dotted(inUse(database), database.Comment),
			section: sectionDatabases,
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
		})
	}
	c.picker = c.withRows(rows)
	c.hints = map[string]string{
		sectionDatabases: "space picks one",
		sectionSchemas:   "space ticks, several are fine",
	}
	return c.marked()
}

func inUse(database driver.Database) string {
	if database.Current {
		return "in use"
	}
	return ""
}

// pick answers space: a database replaces whichever was chosen, a schema joins
// or leaves the set.
func (c catalog) pick() catalog {
	chosen, ok := c.selected()
	if !ok {
		return c
	}
	switch chosen.section {
	case sectionDatabases:
		c.database = chosen.label
	case sectionSchemas:
		schemas := map[string]bool{}
		for name, on := range c.schemas {
			schemas[name] = on
		}
		schemas[chosen.label] = !schemas[chosen.label]
		c.schemas = schemas
	}
	return c.marked()
}

// marked redraws the boxes from what the form holds, which is the only place
// the state of the form is shown.
func (c catalog) marked() catalog {
	rows := make([]row, len(c.rows))
	copy(rows, c.rows)
	for i := range rows {
		switch rows[i].section {
		case sectionDatabases:
			rows[i].mark = ui.Radio(rows[i].label == c.database)
		case sectionSchemas:
			rows[i].mark = ui.Checkbox(c.schemas[rows[i].label])
			rows[i].on = c.schemas[rows[i].label]
		}
	}
	c.picker = c.withRows(rows)
	return c
}

// chosen is the schema filter the form will save, in the order the server
// listed them rather than the order they were ticked.
func (c catalog) chosen() []string {
	picked := make([]string, 0, len(c.schemas))
	for _, item := range c.rows {
		if item.section == sectionSchemas && c.schemas[item.label] {
			picked = append(picked, item.label)
		}
	}
	return picked
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
	m.catalog = m.catalog.withCatalog(catalogMsg{}, m.session.Connection)
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
	case key.Matches(msg, m.keys.Pick):
		m.catalog = m.catalog.pick()
	case key.Matches(msg, m.keys.Choose):
		return m.applyCatalog()
	}
	return m, nil
}

// applyCatalog is the save. A form that was only looked at changes nothing,
// because reconnecting to where you already are is a cost with no answer.
func (m Model) applyCatalog() (tea.Model, tea.Cmd) {
	database, schemas := m.catalog.database, m.catalog.chosen()
	elsewhere := database != "" && database != m.catalog.current
	m.view = viewDashboard
	if !elsewhere && slices.Equal(schemas, m.session.Connection.Filter()) {
		return m, nil
	}
	m.failure = ""
	m.session.Connection.Schemas = schemas
	if len(schemas) > 0 && !slices.Contains(schemas, m.session.Connection.DefaultSchema) {
		m.session.Connection.DefaultSchema = schemas[0]
	}
	m.loading = true
	schema := m.session.Connection.DefaultSchema
	if elsewhere {
		return m, tea.Batch(m.openDatabase(database, schema, schemas), m.spinner.Tick)
	}
	said := m.notify(scope(schemas))
	return m, tea.Batch(m.remember(database, schema, schemas), m.load(), m.spinner.Tick, said)
}

// scope says what the form widened or narrowed the session to, in the words of
// what will be listed rather than of what was ticked.
func scope(schemas []string) string {
	if len(schemas) == 0 {
		return "reading every schema"
	}
	return "reading " + strings.Join(schemas, ", ")
}

// remember writes the place you moved to back to the profile, so the next run
// opens where you left off. Failing to write it is worth saying and nothing
// more: the session is already there.
func (m Model) remember(database, schema string, schemas []string) tea.Cmd {
	workspace := m.workspace
	name := m.session.Connection.Name
	return func() tea.Msg {
		if err := workspace.Remember(name, database, schema, schemas); err != nil {
			return rememberedMsg{err: err}
		}
		return rememberedMsg{}
	}
}

type rememberedMsg struct{ err error }

// openDatabase reconnects, carrying the form over the profile the new session
// was built from, so what the form chose is what gets read and written back.
func (m Model) openDatabase(database, schema string, schemas []string) tea.Cmd {
	workspace := m.workspace
	name := m.session.Connection.Name
	return func() tea.Msg {
		session, cleanup, err := workspace.OpenDatabase(context.Background(), name, database)
		if err == nil {
			session.Connection.DefaultSchema = schema
			session.Connection.Schemas = schemas
		}
		return switchedMsg{session: session, cleanup: cleanup, err: err}
	}
}
