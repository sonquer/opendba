package ui

import (
	"strings"
	"testing"
)

// A statement comes apart into the runs that are drawn in one colour.
func TestAStatementComesApartIntoItsPieces(t *testing.T) {
	for _, want := range []struct {
		name  string
		line  string
		kinds []kind
		texts []string
	}{
		{
			name:  "a select",
			line:  "SELECT id",
			kinds: []kind{kindKeyword, kindPlain, kindPlain},
			texts: []string{"SELECT", " ", "id"},
		},
		{
			name:  "lower case is still a keyword",
			line:  "select",
			kinds: []kind{kindKeyword},
		},
		{
			name:  "a word that only looks like one",
			line:  "selected",
			kinds: []kind{kindPlain},
		},
		{
			name:  "a number",
			line:  "42",
			kinds: []kind{kindNumber},
		},
		{
			name:  "a fraction",
			line:  "3.5",
			kinds: []kind{kindNumber},
			texts: []string{"3.5"},
		},
		{
			name:  "a string",
			line:  "'text'",
			kinds: []kind{kindString},
		},
		{
			name:  "a quote inside a string",
			line:  "'it''s'",
			kinds: []kind{kindString},
			texts: []string{"'it''s'"},
		},
		{
			name:  "a string nobody closed",
			line:  "'open",
			kinds: []kind{kindString},
			texts: []string{"'open"},
		},
		{
			name:  "a quoted name",
			line:  `"Orders"`,
			kinds: []kind{kindString},
		},
		{
			name:  "a line comment",
			line:  "-- why",
			kinds: []kind{kindComment},
			texts: []string{"-- why"},
		},
		{
			name:  "a block comment",
			line:  "/* why */ id",
			kinds: []kind{kindComment, kindPlain, kindPlain},
			texts: []string{"/* why */", " ", "id"},
		},
		{
			name:  "a block comment nobody closed",
			line:  "/* why",
			kinds: []kind{kindComment},
			texts: []string{"/* why"},
		},
		{
			name:  "punctuation",
			line:  "(*)",
			kinds: []kind{kindPunctuation, kindPunctuation, kindPunctuation},
		},
		{
			name:  "a minus that is not a comment",
			line:  "1-2",
			kinds: []kind{kindNumber, kindPunctuation, kindNumber},
		},
		{
			name:  "something else entirely",
			line:  "#",
			kinds: []kind{kindPlain},
		},
	} {
		t.Run(want.name, func(t *testing.T) {
			pieces := tokens(want.line)
			if len(pieces) != len(want.kinds) {
				t.Fatalf("pieces = %+v, want %d of them", pieces, len(want.kinds))
			}
			for i, piece := range pieces {
				if piece.kind != want.kinds[i] {
					t.Errorf("piece %d %q is a %d, want %d",
						i, piece.text, piece.kind, want.kinds[i])
				}
				if want.texts != nil && piece.text != want.texts[i] {
					t.Errorf("piece %d = %q, want %q", i, piece.text, want.texts[i])
				}
			}
		})
	}
}

// Nothing is lost on the way through: what goes in comes out, with colour
// around it.
func TestHighlightingChangesNothingButTheColour(t *testing.T) {
	theme := Default()
	for _, line := range []string{
		"", "SELECT * FROM users WHERE id = 10",
		"-- a comment", "'a string' AND 42", "  indented",
		`INSERT INTO "T" VALUES ('a''b', 1.5)`,
	} {
		if got := ansiStripped(theme.Highlight(line)); got != line {
			t.Errorf("Highlight(%q) reads as %q", line, got)
		}
	}
	if theme.Highlight("") != "" {
		t.Error("an empty line is drawn as one")
	}
	if !strings.Contains(theme.Highlight("SELECT"), "\x1b[") {
		t.Error("a keyword must actually be coloured")
	}
}

// The list of keywords is what somebody typing a statement types.
func TestTheKeywordsAreTheOnesPeopleType(t *testing.T) {
	for _, word := range []string{"SELECT", "select", "Where", "GROUP", "returning"} {
		if !Keyword(word) {
			t.Errorf("%q is a keyword", word)
		}
	}
	for _, word := range []string{"users", "id", "", "selecting"} {
		if Keyword(word) {
			t.Errorf("%q is not", word)
		}
	}
}

func ansiStripped(s string) string {
	var out strings.Builder
	inside := false
	for _, r := range s {
		switch {
		case r == '\x1b':
			inside = true
		case inside && (r == 'm'):
			inside = false
		case !inside:
			out.WriteRune(r)
		}
	}
	return out.String()
}
