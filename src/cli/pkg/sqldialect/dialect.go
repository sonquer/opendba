// Package sqldialect turns SQL text into the facts a safety policy needs.
//
// Each dialect owns a grammar generated from the upstream ANTLR definition of
// its database. Classification walks the parse tree with an allow-list of rules:
// a statement whose shape is not on the list is reported as unrecognised rather
// than guessed at, and a mutating rule found anywhere inside a statement marks
// the whole statement as mutating, which is what catches writes hidden in
// common table expressions.
package sqldialect

import (
	"fmt"
	"sort"
	"strings"
)

// Kind is the dialect independent classification of a statement.
type Kind string

const (
	KindUnknown     Kind = "UNKNOWN"
	KindSelect      Kind = "SELECT"
	KindExplain     Kind = "EXPLAIN"
	KindShow        Kind = "SHOW"
	KindSet         Kind = "SET"
	KindInsert      Kind = "INSERT"
	KindUpdate      Kind = "UPDATE"
	KindDelete      Kind = "DELETE"
	KindMerge       Kind = "MERGE"
	KindTruncate    Kind = "TRUNCATE"
	KindCreate      Kind = "CREATE"
	KindAlter       Kind = "ALTER"
	KindDrop        Kind = "DROP"
	KindGrant       Kind = "GRANT"
	KindRevoke      Kind = "REVOKE"
	KindCopy        Kind = "COPY"
	KindCall        Kind = "CALL"
	KindMaintenance Kind = "MAINTENANCE"
	KindTransaction Kind = "TRANSACTION"
	KindCursor      Kind = "CURSOR"
	KindPrepared    Kind = "PREPARED"
	KindSession     Kind = "SESSION"
	KindPragma      Kind = "PRAGMA"
)

// Statement is what one parsed statement means for safety: the kind it is, what
// it would change, and whether tui4db refuses to run it at all.
type Statement struct {
	Kind         Kind
	MutatedBy    Kind
	Mutating     bool
	Locking      bool
	Materializes bool
	Refusal      string
	Text         string
}

// Reads reports whether the statement only reads: it changes nothing, takes no
// locks, creates no relation and is not refused.
func (s Statement) Reads() bool {
	return !s.Mutating && !s.Locking && !s.Materializes && s.Refusal == ""
}

// SyntaxError is a position and message reported by the parser.
type SyntaxError struct {
	Line    int
	Column  int
	Message string
}

// Error renders the position and the message.
func (e SyntaxError) Error() string {
	return fmt.Sprintf("line %d:%d %s", e.Line, e.Column, e.Message)
}

// Analysis is the result of parsing a request: every statement it contains, and
// every syntax error the parser reported.
type Analysis struct {
	Statements []Statement
	Errors     []SyntaxError
}

// Valid reports whether the request parsed without errors.
func (a Analysis) Valid() bool { return len(a.Errors) == 0 }

// FirstError returns the first syntax error as text, or an empty string.
func (a Analysis) FirstError() string {
	if len(a.Errors) == 0 {
		return ""
	}
	return a.Errors[0].Error()
}

// Dialect parses SQL for one database and reports what its statements do.
type Dialect interface {
	Name() string
	Analyze(sql string) Analysis
}

// Factory maps driver names to dialects.
type Factory struct {
	dialects map[string]Dialect
}

// NewFactory returns a factory holding the given dialects.
func NewFactory(dialects ...Dialect) *Factory {
	factory := &Factory{dialects: map[string]Dialect{}}
	for _, dialect := range dialects {
		factory.Register(dialect)
	}
	return factory
}

// Default returns a factory holding every dialect tui4db ships with.
func Default() *Factory { return NewFactory(PostgreSQL(), SQLite()) }

// Register adds a dialect, replacing any dialect registered under the same name.
func (f *Factory) Register(dialect Dialect) { f.dialects[strings.ToLower(dialect.Name())] = dialect }

// Get returns the dialect registered under the name, ignoring case.
func (f *Factory) Get(name string) (Dialect, error) {
	dialect, ok := f.dialects[strings.ToLower(name)]
	if !ok {
		return nil, fmt.Errorf("no sql dialect registered for %q, have %s", name, strings.Join(f.Names(), ", "))
	}
	return dialect, nil
}

// Names returns the registered dialect names in alphabetical order.
func (f *Factory) Names() []string {
	names := make([]string, 0, len(f.dialects))
	for name := range f.dialects {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
