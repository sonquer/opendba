// Package sqlguard decides whether a SQL request is allowed to reach a database.
//
// Classification is not keyword matching. The request is parsed with the real
// grammar of the target dialect and the decision is made from the parse tree, so
// a DELETE hidden inside a common table expression, a SELECT that takes row
// locks, and an EXPLAIN ANALYZE that actually runs its statement are all
// recognised for what they are.
//
// The guard is one layer of four. The others live in the database role, in the
// pinned session and in the read-only transaction wrapped around every read.
// A mistake here is meant to be caught there.
package sqlguard

import (
	"fmt"

	"github.com/sonquer/tui4db/src/cli/pkg/sqldialect"
)

// Mode is the access mode a connection was configured with.
type Mode string

const (
	ModeReadOnly  Mode = "readonly"
	ModeReadWrite Mode = "readwrite"
)

// Label returns the mode as it is shown to the user.
func (m Mode) Label() string {
	if m == ModeReadWrite {
		return "READ / WRITE"
	}
	return "READ ONLY"
}

// Writable reports whether statements that change the database may run at all.
// An unknown mode is treated as read only.
func (m Mode) Writable() bool { return m == ModeReadWrite }

// Verdict is what the guard decided about a statement.
type Verdict string

const (
	Allow Verdict = "allowed"
	Warn  Verdict = "needs confirmation"
	Block Verdict = "blocked"
)

// Result is the outcome of classifying one request, including the reason to show
// the user and the number of statements the request contained.
type Result struct {
	Verdict    Verdict
	Kind       sqldialect.Kind
	Reason     string
	Statements int
}

// Allowed reports whether the statement may be sent to the server as it is.
func (r Result) Allowed() bool { return r.Verdict == Allow }

// Blocked reports whether the statement must not reach the server.
func (r Result) Blocked() bool { return r.Verdict == Block }

// NeedsConfirmation reports whether the statement may run, but only after the
// user has explicitly confirmed it.
func (r Result) NeedsConfirmation() bool { return r.Verdict == Warn }

// Guard decides whether a request may reach the database. It is the first of
// four layers; the database role, the pinned session and the read-only
// transaction are the other three, and none of them trusts this one.
type Guard struct {
	dialect sqldialect.Dialect
}

// New returns a guard that classifies statements with the given dialect.
func New(dialect sqldialect.Dialect) Guard { return Guard{dialect: dialect} }

// Dialect returns the name of the dialect used to parse statements.
func (g Guard) Dialect() string { return g.dialect.Name() }

// Classify parses the request and decides what may happen to it.
//
// The decision is default deny: a request that does not parse, that is empty,
// that holds more than one statement, or whose statement is not recognised is
// blocked. A statement that only reads is allowed. A statement that writes,
// takes locks or creates a relation is blocked in read only mode and needs
// confirmation otherwise. Statements that tui4db never runs, such as those that
// manage transactions or the session, are blocked in every mode.
func (g Guard) Classify(sql string, mode Mode) Result {
	analysis := g.dialect.Analyze(sql)
	if !analysis.Valid() {
		return Result{
			Verdict: Block,
			Kind:    sqldialect.KindUnknown,
			Reason:  "the statement does not parse: " + analysis.FirstError(),
		}
	}
	switch len(analysis.Statements) {
	case 0:
		return Result{Verdict: Block, Kind: sqldialect.KindUnknown, Reason: "there is no statement to run"}
	case 1:
		result := decide(analysis.Statements[0], mode)
		result.Statements = 1
		return result
	default:
		return Result{
			Verdict:    Block,
			Kind:       analysis.Statements[0].Kind,
			Reason:     fmt.Sprintf("%d statements in one request are never executed together", len(analysis.Statements)),
			Statements: len(analysis.Statements),
		}
	}
}

func decide(statement sqldialect.Statement, mode Mode) Result {
	result := Result{Kind: statement.Kind}
	switch {
	case statement.Refusal != "":
		result.Verdict = Block
		result.Reason = statement.Refusal
	case statement.Reads():
		result.Verdict = Allow
		result.Reason = "reads only"
	case !mode.Writable():
		result.Verdict = Block
		result.Reason = hazard(statement) + "; this connection is READ ONLY"
	default:
		result.Verdict = Warn
		result.Reason = hazard(statement)
	}
	return result
}

func hazard(statement sqldialect.Statement) string {
	switch {
	case statement.Materializes:
		return "the statement creates a new relation"
	case statement.Locking:
		return "the statement takes row locks"
	case statement.MutatedBy != "":
		return "the statement contains " + string(statement.MutatedBy)
	default:
		return string(statement.Kind) + " modifies data or database state"
	}
}
