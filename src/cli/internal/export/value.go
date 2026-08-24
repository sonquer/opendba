package export

import (
	"database/sql/driver"
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// Text writes a value the way a file should hold it, which is not the way a
// table should show it: nothing is truncated, nothing is folded onto one line,
// and a null is an empty field rather than a glyph standing in for one.
func Text(value any) string {
	switch held := value.(type) {
	case nil:
		return ""
	case string:
		return held
	case []byte:
		return readable(held)
	case time.Time:
		return held.Format(time.RFC3339Nano)
	case bool:
		return strconv.FormatBool(held)
	case float32:
		return strconv.FormatFloat(float64(held), 'g', -1, 32)
	case float64:
		return strconv.FormatFloat(held, 'g', -1, 64)
	case int:
		return strconv.Itoa(held)
	case int32:
		return strconv.FormatInt(int64(held), 10)
	case int64:
		return strconv.FormatInt(held, 10)
	case []any:
		return array(held)
	case driver.Valuer:
		return Text(unwrapped(held))
	default:
		return fmt.Sprintf("%v", held)
	}
}

// Value is the same value as something a JSON or a spreadsheet writer can hold
// as itself, so a number stays a number and only what has no equivalent becomes
// text.
func Value(value any) any {
	switch held := value.(type) {
	case nil:
		return nil
	case string, bool, int, int32, int64, float32, float64:
		return held
	case []byte:
		return readable(held)
	case time.Time:
		return held.Format(time.RFC3339Nano)
	case []any:
		return array(held)
	case driver.Valuer:
		return Value(unwrapped(held))
	default:
		return fmt.Sprintf("%v", held)
	}
}

// readable writes a column of bytes as itself when it is text, and as base64
// when it is not.
func readable(held []byte) string {
	if utf8.Valid(held) {
		return string(held)
	}
	return base64.StdEncoding.EncodeToString(held)
}

// array writes a column of values the way the server prints one, because a
// reader who sees {a,b} knows what it was and a reader who sees [a b] does not.
func array(values []any) string {
	written := make([]string, 0, len(values))
	for _, value := range values {
		written = append(written, element(value))
	}
	return "{" + strings.Join(written, ",") + "}"
}

// element quotes what would otherwise be read as the punctuation of the array
// around it.
func element(value any) string {
	if value == nil {
		return "NULL"
	}
	written := Text(value)
	if written == "" || strings.ContainsAny(written, `{},"\ `) {
		return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(written) + `"`
	}
	return written
}

// unwrapped is what a value that knows how to write itself as a standard type
// writes, or the value itself when it will not say.
func unwrapped(value driver.Valuer) any {
	written, err := value.Value()
	if err != nil {
		return fmt.Sprintf("%v", value)
	}
	return written
}
