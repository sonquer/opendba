package local

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/sonquer/opendba/src/cli/internal/ai"
)

// A dialect is the shape a model writes a tool call in.
type dialect struct {
	name  string
	open  string
	close string
	parse func(body string, index int) (ai.ToolCall, error)

	// resultOpen and resultClose are how a tool's answer is handed back.
	resultOpen  string
	resultClose string

	// thinkOpen and thinkClose bracket what a model says to itself on its way to an
	// answer.
	thinkOpen  string
	thinkClose string

	// shape is the one example of a call the model is given. Nothing else in
	// the instructions matters as much: it writes what it is shown.
	shape string

	// arguments writes a set of arguments back out in this dialect, for the
	// calls the model has already made and is being reminded of.
	arguments func(map[string]any) string
}

// GemmaOpen and GemmaClose bracket a call in the dialect the Gemma family
// writes.
const (
	GemmaOpen  = "<|tool_call>"
	GemmaClose = "<tool_call|>"

	// GemmaResultOpen and GemmaResultClose bracket what a tool gave back, in
	// the same family's dialect.
	GemmaResultOpen  = "<|tool_response>"
	GemmaResultClose = "<tool_response|>"

	// gemmaQuote is what that dialect puts around a string, in place of the
	// quotation marks JSON would use.
	gemmaQuote = `<|"|>`

	// gemmaCall is the word between the opening bracket and the tool's name.
	gemmaCall = "call:"

	// gemmaDialect is what the catalogue calls this family.
	gemmaDialect = "gemma"

	// GemmaThinkOpen and GemmaThinkClose bracket the channel this family
	// thinks out loud on, in the same bracket grammar as its tool calls.
	GemmaThinkOpen  = "<|channel>"
	GemmaThinkClose = "<channel|>"
)

// dialects are the shapes read, in the order they are looked for.
func dialects() []dialect {
	return []dialect{
		{
			name: "json", open: CallOpen, close: CallClose, parse: decodeCall,
			resultOpen: ResultOpen, resultClose: ResultClose,
			shape:     `{"name": "the_tool", "arguments": {"an_argument": "a value"}}`,
			arguments: jsonArguments,
		},
		{
			name: gemmaDialect, open: GemmaOpen, close: GemmaClose, parse: decodeGemma,
			resultOpen: GemmaResultOpen, resultClose: GemmaResultClose,
			thinkOpen: GemmaThinkOpen, thinkClose: GemmaThinkClose,
			shape:     `call:the_tool{an_argument:` + gemmaQuote + `a value` + gemmaQuote + `}`,
			arguments: gemmaArguments,
		},
	}
}

// Spoken is the dialect a model writes in, named by the catalogue.
func Spoken(name string) dialect {
	for _, spoken := range dialects() {
		if spoken.name == name {
			return spoken
		}
	}
	return dialects()[0]
}

// jsonArguments writes arguments the way our own dialect carries them.
func jsonArguments(arguments map[string]any) string {
	encoded, err := json.Marshal(arguments)
	if err != nil {
		return "{}"
	}
	return string(encoded)
}

// gemmaArguments writes arguments the way the Gemma family carries them, which
// is the same small language the parser reads.
func gemmaArguments(arguments map[string]any) string {
	names := make([]string, 0, len(arguments))
	for name := range arguments {
		names = append(names, name)
	}
	sort.Strings(names)
	pairs := make([]string, 0, len(names))
	for _, name := range names {
		pairs = append(pairs, name+":"+gemmaWritten(arguments[name]))
	}
	return "{" + strings.Join(pairs, ",") + "}"
}

func gemmaWritten(value any) string {
	switch held := value.(type) {
	case nil:
		return "null"
	case string:
		return gemmaQuote + held + gemmaQuote
	case bool:
		return strconv.FormatBool(held)
	case float64:
		return strconv.FormatFloat(held, 'f', -1, 64)
	case int:
		return strconv.Itoa(held)
	case map[string]any:
		return gemmaArguments(held)
	case []any:
		items := make([]string, 0, len(held))
		for _, item := range held {
			items = append(items, gemmaWritten(item))
		}
		return "[" + strings.Join(items, ",") + "]"
	default:
		return gemmaQuote + fmt.Sprintf("%v", held) + gemmaQuote
	}
}

// Openers is every marker that begins a tool call, which is what the screen has
// to watch for so that a call is never shown to the person as though the model
// were talking to them.
func Openers() []string {
	open := make([]string, 0, 2)
	for _, spoken := range dialects() {
		open = append(open, spoken.open)
	}
	return open
}

// Thinking is every marker that begins or ends what a model says to itself,
// which the screen has to watch for so that a model's deliberation is folded
// away rather than printed as its reply, and so that neither bracket is drawn
// one character at a time on its way to being recognised.
func Thinking() []string {
	markers := make([]string, 0, 2)
	for _, spoken := range dialects() {
		if spoken.thinkOpen == "" {
			continue
		}
		markers = append(markers, spoken.thinkOpen, spoken.thinkClose)
	}
	return markers
}

// thinkingOpens is where the first think-opener begins and which family wrote
// it, or minus one for none.
func thinkingOpens(text string) (dialect, int) {
	found, at := dialect{}, -1
	for _, spoken := range dialects() {
		if spoken.thinkOpen == "" {
			continue
		}
		start := strings.Index(text, spoken.thinkOpen)
		if start < 0 {
			continue
		}
		if at < 0 || start < at {
			found, at = spoken, start
		}
	}
	return found, at
}

// decodeGemma reads a call written the way the Gemma family writes one:
// call:read_table{table:<|"|>orders<|"|>,limit:20} The arguments are a small
// language of its own rather than JSON.
func decodeGemma(body string, index int) (ai.ToolCall, error) {
	body = strings.TrimSpace(body)
	if !strings.HasPrefix(body, gemmaCall) {
		return ai.ToolCall{}, fmt.Errorf("a tool call does not say what it is calling")
	}
	body = body[len(gemmaCall):]
	at := strings.Index(body, "{")
	if at < 0 {
		return ai.ToolCall{}, fmt.Errorf("a tool call has no arguments, not even none")
	}
	name := strings.TrimSpace(body[:at])
	if name == "" {
		return ai.ToolCall{}, fmt.Errorf("a tool call has no name")
	}
	arguments, rest, err := gemmaObject(body[at:], false)
	if err != nil {
		return ai.ToolCall{}, err
	}
	if strings.TrimSpace(rest) != "" {
		return ai.ToolCall{}, fmt.Errorf("a tool call has %q after its arguments", strings.TrimSpace(rest))
	}
	return ai.ToolCall{ID: fmt.Sprintf("call_%d", index+1), Name: name, Arguments: arguments}, nil
}

// gemmaObject reads a braced list of pairs and returns what is left after it.
func gemmaObject(text string, quotedKeys bool) (map[string]any, string, error) {
	rest, ok := strings.CutPrefix(strings.TrimSpace(text), "{")
	if !ok {
		return nil, "", fmt.Errorf("a set of arguments does not begin with a brace")
	}
	out := map[string]any{}
	for {
		rest = strings.TrimSpace(rest)
		if after, done := strings.CutPrefix(rest, "}"); done {
			return out, after, nil
		}
		if len(out) > 0 {
			next, ok := strings.CutPrefix(rest, ",")
			if !ok {
				return nil, "", fmt.Errorf("arguments are not separated by a comma")
			}
			rest = strings.TrimSpace(next)
		}
		key, after, err := gemmaKey(rest, quotedKeys)
		if err != nil {
			return nil, "", err
		}
		rest, ok = strings.CutPrefix(strings.TrimSpace(after), ":")
		if !ok {
			return nil, "", fmt.Errorf("argument %q has no value", key)
		}
		value, left, err := gemmaValue(rest)
		if err != nil {
			return nil, "", err
		}
		out[key] = value
		rest = left
	}
}

func gemmaKey(text string, quoted bool) (string, string, error) {
	if after, ok := strings.CutPrefix(text, gemmaQuote); ok {
		return gemmaString(after)
	}
	if quoted {
		return "", "", fmt.Errorf("an argument name is not bracketed")
	}
	at := strings.IndexAny(text, ":,}")
	if at <= 0 {
		return "", "", fmt.Errorf("an argument has no name")
	}
	return strings.TrimSpace(text[:at]), text[at:], nil
}

// gemmaString reads to the closing bracket. Nothing inside is escaped, so the
// first closing bracket ends it, which is the whole of the rule.
func gemmaString(text string) (string, string, error) {
	at := strings.Index(text, gemmaQuote)
	if at < 0 {
		return "", "", fmt.Errorf("a string in a tool call was never closed")
	}
	return text[:at], text[at+len(gemmaQuote):], nil
}

// gemmaValue reads one value and returns what is left after it.
func gemmaValue(text string) (any, string, error) {
	text = strings.TrimSpace(text)
	switch {
	case strings.HasPrefix(text, gemmaQuote):
		return firstOf(gemmaString(strings.TrimPrefix(text, gemmaQuote)))
	case strings.HasPrefix(text, "{"):
		object, rest, err := gemmaObject(text, false)
		return object, rest, err
	case strings.HasPrefix(text, "["):
		return gemmaArray(text)
	case strings.HasPrefix(text, "true"):
		return true, text[len("true"):], nil
	case strings.HasPrefix(text, "false"):
		return false, text[len("false"):], nil
	case strings.HasPrefix(text, "null"):
		return nil, text[len("null"):], nil
	}
	at := strings.IndexAny(text, ",}]")
	if at < 0 {
		at = len(text)
	}
	word := strings.TrimSpace(text[:at])
	if word == "" {
		return nil, "", fmt.Errorf("an argument in a tool call has no value")
	}
	number, err := strconv.ParseFloat(word, 64)
	if err == nil {
		return number, text[at:], nil
	}
	if strings.ContainsAny(word, " \t\n") {
		return nil, "", fmt.Errorf("the value %q in a tool call is neither a number nor bracketed", word)
	}
	return word, text[at:], nil
}

func gemmaArray(text string) (any, string, error) {
	rest := strings.TrimPrefix(strings.TrimSpace(text), "[")
	items := []any{}
	for {
		rest = strings.TrimSpace(rest)
		if after, done := strings.CutPrefix(rest, "]"); done {
			return items, after, nil
		}
		if len(items) > 0 {
			next, ok := strings.CutPrefix(rest, ",")
			if !ok {
				return nil, "", fmt.Errorf("a list in a tool call is not separated by commas")
			}
			rest = next
		}
		item, left, err := gemmaValue(rest)
		if err != nil {
			return nil, "", err
		}
		items = append(items, item)
		rest = left
	}
}

// firstOf turns a string reader into a value reader, which is the only place
// the two shapes differ.
func firstOf(text, rest string, err error) (any, string, error) { return text, rest, err }
