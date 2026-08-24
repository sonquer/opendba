package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/sonquer/tui4db/src/cli/internal/driver"
)

// sessionsQuery reads what the server is holding.
const sessionsQuery = `SELECT pid, coalesce(usename, ''), coalesce(application_name, ''),
	coalesce(host(client_addr), 'local'), coalesce(datname, ''), coalesce(state, ''),
	coalesce(wait_event_type, ''),
	greatest(coalesce(extract(epoch FROM clock_timestamp() - coalesce(query_start, backend_start)), 0), 0),
	coalesce(query, ''),
	pid = pg_backend_pid()
FROM pg_stat_activity
WHERE coalesce(backend_type, 'client backend') = 'client backend'
ORDER BY state = 'active' DESC, pid = pg_backend_pid(), coalesce(query_start, backend_start)`

func (c *connection) Sessions(ctx context.Context) ([]driver.Session, error) {
	var sessions []driver.Session
	err := c.each(ctx, sessionsQuery, func(rows pgx.Rows) error {
		var (
			session driver.Session
			pid     int64
			seconds float64
		)
		if err := rows.Scan(&pid, &session.User, &session.Application, &session.Client,
			&session.Database, &session.State, &session.Wait, &seconds,
			&session.Statement, &session.Mine); err != nil {
			return err
		}
		session.ID = fmt.Sprintf("%d", pid)
		session.Duration = time.Duration(seconds * float64(time.Second))
		sessions = append(sessions, session)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("list the sessions: %w", err)
	}
	return sessions, nil
}

// Stop asks the server to end what a session is doing.
func (c *connection) Stop(ctx context.Context, id string, terminate bool) error {
	query := "SELECT pg_cancel_backend($1::int)"
	verb := "cancel"
	if terminate {
		query = "SELECT pg_terminate_backend($1::int)"
		verb = "terminate"
	}
	var signalled bool
	if err := c.db.QueryRow(ctx, query, id).Scan(&signalled); err != nil {
		return fmt.Errorf("%s session %s: %w", verb, id, err)
	}
	if !signalled {
		return fmt.Errorf("%s session %s: the server did not signal it, and it may already be gone", verb, id)
	}
	return nil
}
