package export

import (
	"encoding/base64"
	"fmt"
	"strconv"
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
	default:
		return fmt.Sprintf("%v", held)
	}
}

// readable writes a column of bytes as itself when it is text, and as base64
// when it is not. A file with a raw byte in the middle of it is a file the next
// program refuses to open.
func readable(held []byte) string {
	if utf8.Valid(held) {
		return string(held)
	}
	return base64.StdEncoding.EncodeToString(held)
}
