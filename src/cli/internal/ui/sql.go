package ui

import (
	"strings"
	"unicode"
)

// kind is what a piece of a statement is, which is all a colour depends on.
type kind int

const (
	kindPlain kind = iota
	kindKeyword
	kindString
	kindNumber
	kindComment
	kindPunctuation
)

// token is one run of a statement that is drawn in one colour.
type token struct {
	text string
	kind kind
}

// Highlight colours one line of SQL for an editor that redraws it as it is
// typed.
//
// It works a line at a time because that is how the editor hands its text over,
// already wrapped. A string or a comment that runs past the end of a line is
// therefore coloured to the end of that line and starts again plain on the
// next, which is wrong only for text nobody writes in a statement they are
// still typing.
func (t *Theme) Highlight(line string) string {
	if line == "" {
		return ""
	}
	var out strings.Builder
	for _, piece := range tokens(line) {
		out.WriteString(t.coloured(piece))
	}
	return out.String()
}

func (t *Theme) coloured(piece token) string {
	switch piece.kind {
	case kindKeyword:
		return t.sql.keyword.Render(piece.text)
	case kindString:
		return t.sql.text.Render(piece.text)
	case kindNumber:
		return t.sql.number.Render(piece.text)
	case kindComment:
		return t.sql.comment.Render(piece.text)
	case kindPunctuation:
		return t.sql.punctuation.Render(piece.text)
	default:
		return t.sql.plain.Render(piece.text)
	}
}

// tokens takes a line apart into the runs that are drawn in one colour.
func tokens(line string) []token {
	runes := []rune(line)
	pieces := make([]token, 0, len(runes)/4+1)
	for at := 0; at < len(runes); {
		piece, next := scan(runes, at)
		pieces = append(pieces, piece)
		at = next
	}
	return pieces
}

func scan(runes []rune, at int) (token, int) {
	switch {
	case comments(runes, at):
		return token{text: string(runes[at:]), kind: kindComment}, len(runes)
	case block(runes, at):
		return scanBlock(runes, at)
	case runes[at] == '\'' || runes[at] == '"' || runes[at] == '`':
		return scanQuoted(runes, at)
	case unicode.IsDigit(runes[at]):
		return scanWhile(runes, at, kindNumber, numeric)
	case letter(runes[at]):
		return scanWord(runes, at)
	case unicode.IsSpace(runes[at]):
		return scanWhile(runes, at, kindPlain, unicode.IsSpace)
	case punctuation(runes[at]):
		return token{text: string(runes[at]), kind: kindPunctuation}, at + 1
	default:
		return token{text: string(runes[at]), kind: kindPlain}, at + 1
	}
}

func comments(runes []rune, at int) bool {
	return runes[at] == '-' && at+1 < len(runes) && runes[at+1] == '-'
}

func block(runes []rune, at int) bool {
	return runes[at] == '/' && at+1 < len(runes) && runes[at+1] == '*'
}

// scanBlock takes a block comment, which ends where it says or at the end of
// the line if it says nowhere.
func scanBlock(runes []rune, at int) (token, int) {
	for i := at + 2; i+1 < len(runes); i++ {
		if runes[i] == '*' && runes[i+1] == '/' {
			return token{text: string(runes[at : i+2]), kind: kindComment}, i + 2
		}
	}
	return token{text: string(runes[at:]), kind: kindComment}, len(runes)
}

// scanQuoted takes a quoted run, doubling being how SQL escapes the quote it
// is quoted with.
func scanQuoted(runes []rune, at int) (token, int) {
	quote := runes[at]
	for i := at + 1; i < len(runes); i++ {
		if runes[i] != quote {
			continue
		}
		if i+1 < len(runes) && runes[i+1] == quote {
			i++
			continue
		}
		return token{text: string(runes[at : i+1]), kind: kindString}, i + 1
	}
	return token{text: string(runes[at:]), kind: kindString}, len(runes)
}

func scanWord(runes []rune, at int) (token, int) {
	piece, next := scanWhile(runes, at, kindPlain, word)
	if Keyword(piece.text) {
		piece.kind = kindKeyword
	}
	return piece, next
}

func scanWhile(runes []rune, at int, of kind, takes func(rune) bool) (token, int) {
	end := at
	for end < len(runes) && takes(runes[end]) {
		end++
	}
	return token{text: string(runes[at:end]), kind: of}, end
}

func letter(r rune) bool { return unicode.IsLetter(r) || r == '_' }

func word(r rune) bool { return letter(r) || unicode.IsDigit(r) || r == '$' }

func numeric(r rune) bool { return unicode.IsDigit(r) || r == '.' || r == '_' }

func punctuation(r rune) bool {
	return strings.ContainsRune("(),;.*+-/<>=!%|&^~[]{}:?", r)
}

// Keyword reports whether a word is one of the words SQL reserves. The list is
// what a person writing a statement types rather than every word every server
// knows: a word coloured as a keyword that is not one is a lie, and a keyword
// left plain is only a keyword left plain.
func Keyword(word string) bool {
	_, ok := keywords[strings.ToUpper(word)]
	return ok
}

var keywords = words(`
ADD ALL ALTER AND ANY AS ASC BEGIN BETWEEN BY CASE CAST CHECK COLLATE COLUMN
COMMIT CONSTRAINT CREATE CROSS CURRENT_DATE CURRENT_TIME CURRENT_TIMESTAMP
CURRENT_USER DATABASE DEFAULT DELETE DESC DISTINCT DO DROP ELSE END ESCAPE
EXCEPT EXISTS EXPLAIN FALSE FETCH FILTER FIRST FOR FOREIGN FROM FULL GRANT
GROUP HAVING IF ILIKE IN INDEX INNER INSERT INTERSECT INTERVAL INTO IS JOIN
KEY LAST LATERAL LEFT LIKE LIMIT MATERIALIZED NATURAL NOT NULL NULLS OFFSET
ON ONLY OR ORDER OUTER OVER PARTITION PRAGMA PRIMARY RECURSIVE REFERENCES
RETURNING REVOKE RIGHT ROLLBACK ROW ROWS SELECT SET SOME TABLE TEMP TEMPORARY
THEN TIES TRUE TRUNCATE UNION UNIQUE UPDATE USING VACUUM VALUES VIEW WHEN
WHERE WINDOW WITH
`)

func words(list string) map[string]struct{} {
	held := map[string]struct{}{}
	for _, word := range strings.Fields(list) {
		held[word] = struct{}{}
	}
	return held
}
