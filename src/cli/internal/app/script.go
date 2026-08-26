package app

import (
	"strconv"
	"strings"

	"github.com/sonquer/opendba/src/cli/pkg/sqldialect"
)

// caretOffset is where the cursor is in the whole buffer, counted in characters
// rather than in bytes, because that is how the parser counts.
func caretOffset(value string, line, column int) int {
	rows := strings.Split(value, "\n")
	if line < 0 {
		return 0
	}
	at := 0
	for i := 0; i < line && i < len(rows); i++ {
		at += len([]rune(rows[i])) + 1
	}
	if line < len(rows) {
		at += min(max(column, 0), len([]rune(rows[line])))
	}
	return at
}

// script is what the editor holds, taken apart: every statement in it, and
// which one the cursor is in.
type script struct {
	statements []sqldialect.Statement
	at         int
	source     string
}

// reading takes the editor apart.
func (m Model) script() script {
	source := m.statement()
	held := script{source: source}
	if m.session.Dialect == nil || strings.TrimSpace(source) == "" {
		return held
	}
	analysis := m.session.Dialect.Analyze(source)
	held.statements = analysis.Statements
	if len(analysis.Statements) < 2 {
		return held
	}
	offset := caretOffset(source, m.editor.Line(), m.editor.Column())
	for i, statement := range analysis.Statements {
		if offset >= statement.Start && offset <= statement.Stop {
			held.at = i
			return held
		}
		if statement.Start <= offset {
			held.at = i
		}
	}
	return held
}

// chosen is the statement that pressing run would send.
func (s script) chosen() string {
	if len(s.statements) < 2 {
		return s.source
	}
	if sliced := s.statements[s.at].Slice(s.source); sliced != "" {
		return sliced
	}
	return s.source
}

// several reports whether the editor holds more than one statement, which is
// the only time it is worth saying which one is about to run.
func (s script) several() bool { return len(s.statements) > 1 }

// place says which statement of a script is about to run.
func (s script) place() string {
	if !s.several() {
		return ""
	}
	return "statement " + strconv.Itoa(s.at+1) + " of " + strconv.Itoa(len(s.statements))
}
