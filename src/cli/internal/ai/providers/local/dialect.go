package local

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/sonquer/tui4db/src/cli/internal/ai"
)

// A dialect is the shape a model writes a tool call in.
//
// There is more than one because a model writes what it was trained to write.
// Telling it otherwise in a system prompt works with the fine-tunes that were
// taught to follow instructions about it and fails with the ones that have a
// format of their own in the weights: Gemma 4 answers a question about the
// database with <|tool_call>call:list_schemas{}<tool_call|> whatever it has
// been asked to do, because that is what a tool call looks like to it.
//
// So both are read. What a model writes is understood rather than corrected,
// and the only thing that would be gained by insisting on one shape is a
// program that puts the model's own words on the screen and calls no tools.
type dialect struct {
	name  string
	open  string
	close string
	parse func(body string, index int) (ai.ToolCall, error)

	// resultOpen and resultClose are how a tool's answer is handed back. They
	// belong with the call because they are the other half of the same
	// conversation: a model shown its own shape asks in it, and a model shown
	// ours asks in ours and then invents both sides of a dialogue it has never
	// seen. That last part is not a guess — it is what one of them did.
	resultOpen  string
	resultClose string

	// shape is the one example of a call the model is given. Nothing else in
	// the instructions matters as much: it writes what it is shown.
	shape string

	// arguments writes a set of arguments back out in this dialect, for the
	// calls the model has already made and is being reminded of.
	arguments func(map[string]any) string
}

// GemmaOpen and GemmaClose bracket a call in the dialect the Gemma family
// writes. The brackets are not a typo: the opening one is <| … > and the
// closing one is < … |>.
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
			shape:     `call:the_tool{an_argument:` + gemmaQuote + `a value` + gemmaQuote + `}`,
			arguments: gemmaArguments,
		},
	}
}

// Spoken is the dialect a model writes in, named by the catalogue. A model that
// has one of its own is talked to in it; everything else is given ours, which
// the instructions describe and which most fine-tunes will follow.
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

// decodeGemma reads a call written the way the Gemma family writes one:
//
//	call:read_table{table:<|"|>orders<|"|>,limit:20}
//
// The arguments are a small language of its own rather than JSON. Strings are
// bracketed instead of quoted and are not escaped, keys are bare words, and
// anything that is not one of the words true, false or null and does not begin
// with a bracket is a number.
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
// Keys are bare at every level in what this dialect writes, and bracketed keys
// are read as well because the format allows them.
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
//
// A bare word is what this dialect writes for anything it has no other shape
// for, and it is written without spaces. One with a space in it is a value that
// has run into whatever was meant to come after it — a missing comma, most
// likely — which is a call to refuse rather than a string to invent.
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
