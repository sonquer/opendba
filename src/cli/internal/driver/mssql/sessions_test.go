package mssql

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

func sessionRows() *sqlmock.Rows {
	return sqlmock.NewRows([]string{
		"spid", "user", "application", "client", "database", "status", "wait", "seconds", "statement", "mine"}).
		AddRow(int64(52), "reader", "opendba/production", "10.0.0.4", "app", "running", "PAGEIOLATCH_SH", int64(90), "SELECT 1", true).
		AddRow(int64(53), "writer", "SqlPackage", "10.0.0.9", "app", "sleeping", "", int64(0), "", false)
}

func TestSessions(t *testing.T) {
	conn, mock := mocked(t, readOnlyConfig())
	mock.ExpectQuery(sessionsQuery).WillReturnRows(sessionRows())

	sessions, err := conn.Sessions(context.Background())
	if err != nil {
		t.Fatalf("Sessions() = %v", err)
	}
	if len(sessions) != 2 || sessions[0].ID != "52" || !sessions[0].Mine {
		t.Errorf("sessions = %+v", sessions)
	}
	if !sessions[0].Running() {
		t.Error("a session running a statement must read as active")
	}
	if sessions[0].Duration != 90*time.Second || sessions[0].Wait != "PAGEIOLATCH_SH" {
		t.Errorf("session = %+v", sessions[0])
	}
	if sessions[1].Running() || sessions[1].State != "idle" {
		t.Errorf("session = %+v", sessions[1])
	}
	if sessions[1].Duration != 0 {
		t.Error("a session that has been waiting no time has waited no time")
	}
}

func TestSessionsFallBackToAListingWithoutTheStatements(t *testing.T) {
	conn, mock := mocked(t, readOnlyConfig())
	mock.ExpectQuery(sessionsQuery).WillReturnError(errRefused)
	mock.ExpectQuery(quietSessionsQuery).WillReturnRows(sessionRows())

	sessions, err := conn.Sessions(context.Background())
	if err != nil {
		t.Fatalf("a login that may not read the statements must still see the sessions: %v", err)
	}
	if len(sessions) != 2 {
		t.Errorf("sessions = %+v", sessions)
	}
}

func TestSessionsFallBackToTheOneViewALoginCanAlwaysRead(t *testing.T) {
	conn, mock := mocked(t, readOnlyConfig())
	mock.ExpectQuery(sessionsQuery).WillReturnError(errRefused)
	mock.ExpectQuery(quietSessionsQuery).WillReturnError(errRefused)
	mock.ExpectQuery(ownSessionQuery).WillReturnRows(sessionRows())

	sessions, err := conn.Sessions(context.Background())
	if err != nil {
		t.Fatalf("Sessions() = %v", err)
	}
	if len(sessions) != 2 {
		t.Errorf("sessions = %+v", sessions)
	}
}

// TestALoginThatMaySeeNoSessionsSeesNoSessions is why this is not an error: the
// activity screen redraws every few seconds, and a login without VIEW SERVER
// STATE would fill it with the same refusal for as long as it stayed open.
func TestALoginThatMaySeeNoSessionsSeesNoSessions(t *testing.T) {
	conn, mock := mocked(t, readOnlyConfig())
	for _, query := range []string{sessionsQuery, quietSessionsQuery, ownSessionQuery} {
		mock.ExpectQuery(query).WillReturnError(errRefused)
	}
	sessions, err := conn.Sessions(context.Background())
	if err != nil {
		t.Fatalf("a refused permission is not a failure: %v", err)
	}
	if len(sessions) != 0 {
		t.Errorf("sessions = %+v", sessions)
	}
}

func TestState(t *testing.T) {
	cases := map[string]string{
		"running": "active", "runnable": "active", "suspended": "active",
		"sleeping": "idle", "dormant": "idle", "preconnect": "preconnect",
	}
	for status, want := range cases {
		if got := State(status); got != want {
			t.Errorf("State(%q) = %q, want %q", status, got, want)
		}
	}
}

func TestStop(t *testing.T) {
	t.Run("ending a session", func(t *testing.T) {
		conn, mock := mocked(t, writableConfig())
		mock.ExpectExec("KILL 52").WillReturnResult(sqlmock.NewResult(0, 0))
		if err := conn.Stop(context.Background(), "52", true); err != nil {
			t.Fatalf("Stop() = %v", err)
		}
	})
	t.Run("a session the server would not end", func(t *testing.T) {
		conn, mock := mocked(t, writableConfig())
		mock.ExpectExec("KILL 52").WillReturnError(errRefused)
		if err := conn.Stop(context.Background(), "52", true); err == nil {
			t.Fatal("want an error")
		}
	})
	t.Run("stopping only the statement", func(t *testing.T) {
		conn, _ := mocked(t, writableConfig())
		err := conn.Stop(context.Background(), "52", false)
		if err == nil {
			t.Fatal("sql server cannot do this, and must say so")
		}
		if !strings.Contains(err.Error(), "without ending the session") {
			t.Errorf("Stop() = %v", err)
		}
	})
	t.Run("something that is not a session", func(t *testing.T) {
		conn, _ := mocked(t, writableConfig())
		for _, id := range []string{"; DROP TABLE users", "0", "-1", ""} {
			if err := conn.Stop(context.Background(), id, true); err == nil {
				t.Errorf("Stop(%q) must be refused before it reaches the server", id)
			}
		}
	})
}

func TestSessionsReportsARowItCannotRead(t *testing.T) {
	conn, mock := mocked(t, readOnlyConfig())
	unreadable := sqlmock.NewRows([]string{"spid", "user", "application", "client",
		"database", "status", "wait", "seconds", "statement", "mine"}).
		AddRow("not a number", "", "", "", "", "", "", int64(0), "", false)
	mock.ExpectQuery(sessionsQuery).WillReturnRows(unreadable)
	mock.ExpectQuery(quietSessionsQuery).WillReturnError(errRefused)
	mock.ExpectQuery(ownSessionQuery).WillReturnRows(sessionRows())

	sessions, err := conn.Sessions(context.Background())
	if err != nil {
		t.Fatalf("Sessions() = %v", err)
	}
	if len(sessions) != 2 {
		t.Errorf("a listing that could not be read falls through to the next: %+v", sessions)
	}
}
