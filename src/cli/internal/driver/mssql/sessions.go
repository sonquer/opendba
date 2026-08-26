package mssql

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"time"

	"github.com/sonquer/opendba/src/cli/internal/driver"
)

// sessionsQuery reads what the server is holding. Without VIEW SERVER STATE
// this returns only the session asking, which is a smaller answer rather than
// an error.
const sessionsQuery = `SELECT s.session_id,
	ISNULL(s.login_name, N''),
	ISNULL(s.program_name, N''),
	ISNULL(c.client_net_address, N'local'),
	ISNULL(DB_NAME(s.database_id), N''),
	ISNULL(s.status, N''),
	ISNULL(r.wait_type, N''),
	DATEDIFF(second, ISNULL(r.start_time, s.last_request_start_time), SYSDATETIME()),
	ISNULL(SUBSTRING(txt.text, (r.statement_start_offset / 2) + 1,
		((CASE r.statement_end_offset WHEN -1 THEN DATALENGTH(txt.text)
		  ELSE r.statement_end_offset END - r.statement_start_offset) / 2) + 1), N''),
	CAST(CASE WHEN s.session_id = @@SPID THEN 1 ELSE 0 END AS bit)
FROM sys.dm_exec_sessions s
LEFT JOIN sys.dm_exec_connections c ON c.session_id = s.session_id
LEFT JOIN sys.dm_exec_requests r ON r.session_id = s.session_id
OUTER APPLY sys.dm_exec_sql_text(r.sql_handle) txt
WHERE s.is_user_process = 1
ORDER BY CASE WHEN r.session_id IS NULL THEN 1 ELSE 0 END, s.session_id`

// ownSessionQuery is the last thing to try: the one view that answers a login
// with no right to see the server at all, which answers with the caller's own
// session and nothing else.
const ownSessionQuery = `SELECT s.session_id,
	ISNULL(s.login_name, N''),
	ISNULL(s.program_name, N''),
	N'local',
	ISNULL(DB_NAME(s.database_id), N''),
	ISNULL(s.status, N''),
	N'',
	DATEDIFF(second, s.last_request_start_time, SYSDATETIME()),
	N'',
	CAST(CASE WHEN s.session_id = @@SPID THEN 1 ELSE 0 END AS bit)
FROM sys.dm_exec_sessions s
WHERE s.is_user_process = 1
ORDER BY s.session_id`

// quietSessionsQuery is the same listing without the statement each session is
// running, for a login that may see the sessions but not their text.
const quietSessionsQuery = `SELECT s.session_id,
	ISNULL(s.login_name, N''),
	ISNULL(s.program_name, N''),
	ISNULL(c.client_net_address, N'local'),
	ISNULL(DB_NAME(s.database_id), N''),
	ISNULL(s.status, N''),
	ISNULL(r.wait_type, N''),
	DATEDIFF(second, ISNULL(r.start_time, s.last_request_start_time), SYSDATETIME()),
	N'',
	CAST(CASE WHEN s.session_id = @@SPID THEN 1 ELSE 0 END AS bit)
FROM sys.dm_exec_sessions s
LEFT JOIN sys.dm_exec_connections c ON c.session_id = s.session_id
LEFT JOIN sys.dm_exec_requests r ON r.session_id = s.session_id
WHERE s.is_user_process = 1
ORDER BY CASE WHEN r.session_id IS NULL THEN 1 ELSE 0 END, s.session_id`

// Sessions asks for as much as this login is allowed to see, and gives up
// quietly rather than loudly. Every one of these views is behind a permission a
// login made for reading tables is rarely given, and a screen that redraws
// itself every few seconds must not fill with the same refusal each time. A
// login that may see nothing sees no sessions, which is what it has.
func (c *connection) Sessions(ctx context.Context) ([]driver.Session, error) {
	for _, query := range []string{sessionsQuery, quietSessionsQuery, ownSessionQuery} {
		sessions, err := c.sessions(ctx, query)
		if err == nil {
			return sessions, nil
		}
	}
	return nil, nil
}

func (c *connection) sessions(ctx context.Context, query string) ([]driver.Session, error) {
	var sessions []driver.Session
	err := c.each(ctx, query, func(rows *sql.Rows) error {
		var (
			session driver.Session
			spid    int64
			status  string
			seconds int64
		)
		if err := rows.Scan(&spid, &session.User, &session.Application, &session.Client,
			&session.Database, &status, &session.Wait, &seconds,
			&session.Statement, &session.Mine); err != nil {
			return err
		}
		session.ID = strconv.FormatInt(spid, 10)
		session.State = State(status)
		if seconds > 0 {
			session.Duration = time.Duration(seconds) * time.Second
		}
		sessions = append(sessions, session)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return sessions, nil
}

// State turns what SQL Server calls a session's status into the one word the
// rest of the program reads: a session is either doing work or waiting for its
// client to say something.
func State(status string) string {
	switch status {
	case "running", "runnable", "suspended":
		return "active"
	case "sleeping", "dormant":
		return "idle"
	default:
		return status
	}
}

// Stop ends what a session is doing. SQL Server has one way to do that and it
// ends the session with it, so there is nothing to offer a caller that only
// wanted the statement stopped.
func (c *connection) Stop(ctx context.Context, id string, terminate bool) error {
	spid, err := strconv.Atoi(id)
	if err != nil || spid <= 0 {
		return fmt.Errorf("terminate session %s: that is not a session number", id)
	}
	if !terminate {
		return fmt.Errorf("sql server cannot stop a statement without ending the session it runs in")
	}
	if _, err := c.db.ExecContext(ctx, "KILL "+strconv.Itoa(spid)); err != nil {
		return fmt.Errorf("terminate session %s: %w", id, err)
	}
	return nil
}
