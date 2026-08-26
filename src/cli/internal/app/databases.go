package app

import (
	"context"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/sonquer/opendba/src/cli/internal/driver"
)

const catalogTimeout = 15 * time.Second

type catalogMsg struct {
	databases []driver.Database
	schemas   []driver.Schema
	err       error

	// on is the session whose server was read, since a read asked for on one
	// lands while another may be in front.
	on sessionID
}

func (m Model) readCatalog(read link) tea.Cmd {
	conn := read.session.Conn
	on := read.id
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), catalogTimeout)
		defer cancel()

		databases, err := conn.Databases(ctx)
		if err != nil {
			return catalogMsg{on: on, err: err}
		}
		schemas, err := conn.Schemas(ctx)
		if err != nil {
			return catalogMsg{on: on, err: err}
		}
		return catalogMsg{on: on, databases: databases, schemas: schemas}
	}
}

// scope says what the form widened or narrowed a connection to, in the words of
// what will be listed rather than of what was ticked.
func scope(schemas []string) string {
	if len(schemas) == 0 {
		return "reading every schema"
	}
	return "reading " + strings.Join(schemas, ", ")
}

// remember writes the place you moved to back to the profile, so the next run
// opens where you left off. Only the session in front writes: one that is not in
// front is not where you left off, and two sessions on one profile writing at
// once is each of them undoing the other.
func (m Model) remember(profile, database, schema string, schemas []string) tea.Cmd {
	workspace := m.workspace
	return func() tea.Msg {
		if err := workspace.Remember(profile, database, schema, schemas); err != nil {
			return rememberedMsg{err: err}
		}
		return rememberedMsg{}
	}
}

type rememberedMsg struct{ err error }

// openDatabase4Catalog reconnects to another database of the session the form
// belongs to, carrying what the form chose over the profile the new session is
// built from.
func (m Model) openDatabase4Catalog(held *catalog) tea.Cmd {
	workspace := m.workspace
	name := m.linked4Sheet(held.on).session.Connection.Name
	database, schemas := held.database, held.chosen()
	schema := ""
	if len(schemas) > 0 {
		schema = schemas[0]
	}
	return func() tea.Msg {
		session, cleanup, err := workspace.OpenDatabase(context.Background(), name, database)
		if err == nil && len(schemas) > 0 {
			session.Connection.DefaultSchema = schema
			session.Connection.Schemas = schemas
		}
		return switchedMsg{session: session, cleanup: cleanup, err: err, profile: name}
	}
}
