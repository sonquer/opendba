package app

import (
	"strings"
	"testing"

	"github.com/sonquer/opendba/src/cli/pkg/sqldialect"
)

// The cursor is found in the buffer by counting characters, not bytes, or a
// statement with anything but ASCII in it would be cut in the wrong place.
func TestTheCursorIsFoundInTheBuffer(t *testing.T) {
	for _, want := range []struct {
		name         string
		value        string
		line, column int
		at           int
	}{
		{"the start", "SELECT 1", 0, 0, 0},
		{"along the first line", "SELECT 1", 0, 3, 3},
		{"the second line", "SELECT 1\nSELECT 2", 1, 0, 9},
		{"along the second", "SELECT 1\nSELECT 2", 1, 2, 11},
		{"past the end of a line", "SELECT 1\nSELECT 2", 0, 99, 8},
		{"a line that is not there", "SELECT 1", 9, 0, 9},
		{"before the start", "SELECT 1", -1, 0, 0},
		{"characters rather than bytes", "SELECT 'zażółć'\nSELECT 2", 1, 0, 16},
	} {
		t.Run(want.name, func(t *testing.T) {
			if got := caretOffset(want.value, want.line, want.column); got != want.at {
				t.Errorf("caretOffset = %d, want %d", got, want.at)
			}
		})
	}
}

// A buffer holding several statements runs the one the cursor is in, which is
// what makes a script a script. The guard still refuses more than one statement
// in a request: it is handed one rather than talked round.
func TestAScriptRunsTheStatementTheCursorIsIn(t *testing.T) {
	m := typeInto(t, workbench(t), "SELECT 1;")
	m, _ = press(t, m, "enter")
	m = typeInto(t, m, "SELECT 2")

	held := m.script()
	if !held.several() {
		t.Fatalf("statements = %d, that is a script", len(held.statements))
	}
	if got := held.chosen(); got != "SELECT 2" {
		t.Errorf("chosen = %q, the cursor is in the second", got)
	}
	if !strings.Contains(held.place(), "statement 2 of 2") {
		t.Errorf("place = %q", held.place())
	}
	if !m.Verdict().Allowed() {
		t.Errorf("verdict = %+v, one statement is not more than one", m.Verdict())
	}

	for range 4 {
		m, _ = press(t, m, "up")
	}
	if got := m.script().chosen(); got != "SELECT 1" {
		t.Errorf("chosen = %q, the cursor is in the first now", got)
	}
}

// A script sends only what was chosen, and the whole buffer never reaches the
// server.
func TestAScriptSendsOnlyTheStatementItChose(t *testing.T) {
	conn := healthy()
	m := loadedWith(t, conn, workspaceWith(t))
	editing, _ := press(t, m, "e")
	typed := typeInto(t, editing, "SELECT 1;")
	typed, _ = press(t, typed, "enter")
	typed = typeInto(t, typed, "SELECT 2")

	ran, cmd := press(t, typed, "ctrl+r")
	if cmd == nil {
		t.Fatal("running a script must run something")
	}
	done := settle(t, ran, cmd)
	if done.results.statement != "SELECT 2" {
		t.Errorf("statement = %q, only the chosen one is sent", done.results.statement)
	}
}

// One statement is not a script, and says nothing about being one.
func TestOneStatementIsNotAScript(t *testing.T) {
	m := typeInto(t, workbench(t), "SELECT 1")
	held := m.script()
	if held.several() || held.place() != "" {
		t.Errorf("a single statement must not be numbered: %q", held.place())
	}
	if got := held.chosen(); got != "SELECT 1" {
		t.Errorf("chosen = %q", got)
	}
	empty := workbench(t).script()
	if empty.chosen() != "" || empty.several() {
		t.Error("an empty editor holds nothing")
	}
}

// A buffer the parser cannot take apart is sent as it stands, so that what
// refuses it is the guard with its account of why.
func TestABufferThatWillNotParseIsSentAsItStands(t *testing.T) {
	m := typeInto(t, workbench(t), "SELECT FROM WHERE")
	if got := m.script().chosen(); got != "SELECT FROM WHERE" {
		t.Errorf("chosen = %q", got)
	}
	if m.Verdict().Allowed() {
		t.Error("and the guard is what refuses it")
	}
}

// Without a dialect there is nothing to take apart, and the buffer stands.
func TestWithoutADialectTheBufferStands(t *testing.T) {
	m := typeInto(t, workbench(t), "SELECT 1; SELECT 2")
	m.session.Dialect = nil
	if got := m.script().chosen(); got != "SELECT 1; SELECT 2" {
		t.Errorf("chosen = %q", got)
	}
}

// A statement the parser gave no position for is sent as the whole buffer
// rather than as nothing.
func TestAStatementWithNoPositionFallsBackToTheBuffer(t *testing.T) {
	held := script{
		source: "SELECT 1; SELECT 2",
		at:     1,
		statements: []sqldialect.Statement{
			{Start: 0, Stop: 7},
			{Start: 40, Stop: 50},
		},
	}
	if got := held.chosen(); got != "SELECT 1; SELECT 2" {
		t.Errorf("chosen = %q, a span outside the buffer is no span at all", got)
	}
}

// The cursor before the first statement is in the first statement.
func TestTheCursorBeforeTheFirstStatementIsInIt(t *testing.T) {
	m := typeInto(t, workbench(t), "SELECT 1;\nSELECT 2")
	for range 40 {
		m, _ = press(t, m, "left")
	}
	if got := m.script().chosen(); got != "SELECT 1" {
		t.Errorf("chosen = %q", got)
	}
}
