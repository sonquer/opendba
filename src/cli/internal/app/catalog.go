package app

import (
	"slices"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/sonquer/opendba/src/cli/internal/ui"
)

const (
	catalogWidth   = 62
	minCatalogRows = 3
)

// catalogNowMsg opens the databases and schemas from the command list.
type catalogNowMsg struct{}

// catalog is the databases this server lets you open and the schemas of the one
// it is reading. It is a form rather than a menu: one database, any number of
// schemas, and nothing applied until it is saved.
//
// It is pinned to the session it was opened on. What the form is about must not
// change because a result landed somewhere else and moved the connection in
// front while you were reading it.
type catalog struct {
	theme   *ui.Theme
	on      sessionID
	cursor  int
	known   catalogMsg
	loading bool
	trouble string

	// database is the one the form would save and current is the one the session
	// is already reading, which is what tells choosing where you are from
	// choosing somewhere else.
	database string
	current  string
	schemas  map[string]bool
}

// openCatalog puts the databases and schemas of the session in front on screen.
// It never connects: the session is open by definition, which is the whole
// difference between this and the screen it replaced.
func (m Model) openCatalog() (tea.Model, tea.Cmd) {
	connection := m.session.Connection
	built := &catalog{
		theme:    m.theme,
		on:       m.id,
		loading:  true,
		database: connection.Database,
		current:  connection.Database,
		schemas:  map[string]bool{},
	}
	for _, schema := range connection.Filter() {
		built.schemas[schema] = true
	}
	m.catalog = built
	m.editor.Blur()
	return m, m.readCatalog(m.link)
}

// withCatalog takes what the server holds.
func (c *catalog) withCatalog(msg catalogMsg) {
	c.loading = false
	if msg.err != nil {
		c.trouble = msg.err.Error()
		return
	}
	c.trouble, c.known = "", msg
	for _, database := range msg.databases {
		if database.Current {
			c.database, c.current = database.Name, database.Name
		}
	}
}

// rows4Catalog is every database this server lets you open, with the schemas of
// the one that is chosen under it.
func (c *catalog) rows4Catalog() []row {
	rows := make([]row, 0, len(c.known.databases)+len(c.known.schemas))
	for _, database := range c.known.databases {
		chosen := database.Name == c.database
		rows = append(rows, row{
			key:   rowDatabase + database.Name,
			label: database.Name,
			note:  database.Comment,
			mark:  ui.Radio(chosen),
			on:    chosen,
		})
		if !chosen {
			continue
		}
		for _, schema := range c.known.schemas {
			if schema.System {
				continue
			}
			ticked := c.schemas[schema.Name]
			rows = append(rows, row{
				key:   rowSchema + schema.Name,
				label: schema.Name,
				note:  ui.Plural(schema.Tables, "table", "tables"),
				mark:  ui.Checkbox(ticked),
				on:    ticked,
				depth: 1,
			})
		}
	}
	return rows
}

// row keys, so a schema named like a database is still its own row.
const (
	rowDatabase = "db:"
	rowSchema   = "schema:"
)

// chosen is the schema filter the form would save, in the order the server
// listed them rather than the order they were ticked.
func (c *catalog) chosen() []string {
	picked := make([]string, 0, len(c.schemas))
	for _, schema := range c.known.schemas {
		if c.schemas[schema.Name] {
			picked = append(picked, schema.Name)
		}
	}
	return picked
}

func (c *catalog) selected() (row, bool) {
	rows := c.rows4Catalog()
	if c.cursor < 0 || c.cursor >= len(rows) {
		return row{}, false
	}
	return rows[c.cursor], true
}

func (m Model) catalogKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.Leave):
		return m.confirmQuit()
	case key.Matches(msg, m.keys.Back), key.Matches(msg, m.keys.Catalog):
		m.catalog = nil
		return m, nil
	case key.Matches(msg, m.keys.Up):
		m.catalog.move(-1)
	case key.Matches(msg, m.keys.Down):
		m.catalog.move(1)
	case key.Matches(msg, m.keys.Pick):
		m.catalog.pick()
	case key.Matches(msg, m.keys.Choose):
		return m.applied4Catalog()
	}
	return m, nil
}

func (c *catalog) move(step int) {
	rows := c.rows4Catalog()
	if len(rows) == 0 {
		return
	}
	c.cursor = (c.cursor + step + len(rows)) % len(rows)
}

// pick answers space: a database replaces whichever was chosen, a schema joins
// or leaves the set.
func (c *catalog) pick() {
	chosen, ok := c.selected()
	if !ok {
		return
	}
	switch {
	case strings.HasPrefix(chosen.key, rowDatabase):
		c.database = chosen.label
	case strings.HasPrefix(chosen.key, rowSchema):
		c.schemas[chosen.label] = !c.schemas[chosen.label]
	}
}

// applied4Catalog is the save. Choosing the database that is already being read
// changes nothing, because reconnecting to where you already are is a cost with
// no answer.
func (m Model) applied4Catalog() (tea.Model, tea.Cmd) {
	held := m.catalog
	at, open := m.linkOf(held.on)
	if !open {
		m.catalog = nil
		return m, nil
	}
	if held.database != "" && held.database != held.current {
		m.catalog = nil
		m.loading = true
		m.failure = ""
		return m, tea.Batch(m.openDatabase4Catalog(held), m.spinner.Tick)
	}
	return m.filtered4Catalog(m.eachLink()[at])
}

// filtered4Catalog applies the ticked schemas to the session the form belongs
// to, which is a filter over what is listed rather than a reason to reconnect.
func (m Model) filtered4Catalog(filtered link) (tea.Model, tea.Cmd) {
	schemas := m.catalog.chosen()
	m.catalog = nil
	if slices.Equal(schemas, filtered.session.Connection.Filter()) {
		return m, nil
	}
	filtered.session.Connection.Schemas = schemas
	if len(schemas) > 0 && !slices.Contains(schemas, filtered.session.Connection.DefaultSchema) {
		filtered.session.Connection.DefaultSchema = schemas[0]
	}
	filtered.loading, filtered.read = true, false
	m = m.wrote4Link(filtered.id, filtered)
	connection := filtered.session.Connection
	said := []tea.Cmd{m.load(), m.spinner.Tick, m.notify(scope(schemas))}
	if filtered.id == m.id {
		said = append(said, m.remember(profile4Link(connection), connection.Database,
			connection.DefaultSchema, schemas))
	}
	return m, tea.Batch(said...)
}

func (m Model) view4Catalog(width, height int) string {
	inner := min(ui.TextWidth(width)-6, catalogWidth)
	held := m.catalog
	list := newPicker(m.theme, "this server lists no databases")
	list = list.withRows(window4Switch(held.rows4Catalog(), held.cursor, room4Switch(height)))
	list.cursor = min(held.cursor, max(len(list.rows)-1, 0))

	title := ui.SplitLine(m.theme.Title.Render("databases and schemas"),
		m.theme.Subtle.Render(m.label4Link(m.linked4Sheet(held.on))), inner)
	lines := []string{title, m.theme.Rule(inner)}
	switch {
	case held.trouble != "":
		lines = append(lines, m.theme.Error.Render("✗ "+held.trouble))
	case held.loading:
		lines = append(lines, m.theme.Muted.Render("  reading the server"))
	default:
		lines = append(lines, list.view(inner))
	}
	lines = append(lines, "", m.theme.Hints(
		ui.Hint{Key: "space", Does: "pick"},
		ui.Hint{Key: "enter", Does: "save"},
		ui.Hint{Key: "esc", Does: "cancel"}))
	return m.theme.Panel.Render(square(strings.Join(lines, "\n"), inner))
}
