package local

import (
	"reflect"
	"strings"
	"testing"

	"github.com/sonquer/opendba/src/cli/internal/ai"
)

// TestTheGemmaDialectIsRead is the answer that started this: a model asked how
// many tables there are wrote its own tool call, and what the person saw was
// the assistant apparently talking in tags.
func TestTheGemmaDialectIsRead(t *testing.T) {
	calls, prose, err := ParseCalls("I will look.\n<|tool_call>call:list_schemas{}<tool_call|>")
	if err != nil {
		t.Fatalf("ParseCalls() error = %v", err)
	}
	if prose != "I will look." {
		t.Fatalf("prose = %q", prose)
	}
	if len(calls) != 1 || calls[0].Name != "list_schemas" {
		t.Fatalf("calls = %+v, want the one the model made", calls)
	}
	if len(calls[0].Arguments) != 0 {
		t.Fatalf("arguments = %+v, want none", calls[0].Arguments)
	}
}

func TestTheGemmaDialectCarriesArguments(t *testing.T) {
	calls, _, err := ParseCalls(`<|tool_call>call:read_rows{table:<|"|>public.orders<|"|>,limit:20,` +
		`descending:true,nothing:null,columns:[<|"|>id<|"|>,<|"|>total<|"|>],where:{status:<|"|>paid<|"|>}}<tool_call|>`)
	if err != nil {
		t.Fatalf("ParseCalls() error = %v", err)
	}
	if len(calls) != 1 {
		t.Fatalf("calls = %+v", calls)
	}
	want := map[string]any{
		"table":      "public.orders",
		"limit":      float64(20),
		"descending": true,
		"nothing":    nil,
		"columns":    []any{"id", "total"},
		"where":      map[string]any{"status": "paid"},
	}
	if !reflect.DeepEqual(calls[0].Arguments, want) {
		t.Fatalf("arguments = %#v\nwant %#v", calls[0].Arguments, want)
	}
}

// TestAStringInTheGemmaDialectIsNotEscaped is the rule that makes the reading
// simple and is worth pinning: whatever is between the brackets is the value,
// quotation marks, backslashes, braces and all.
func TestAStringInTheGemmaDialectIsNotEscaped(t *testing.T) {
	calls, _, err := ParseCalls(`<|tool_call>call:run{sql:<|"|>SELECT "a\b", {1,2} FROM t<|"|>}<tool_call|>`)
	if err != nil {
		t.Fatalf("ParseCalls() error = %v", err)
	}
	if got := calls[0].Arguments["sql"]; got != `SELECT "a\b", {1,2} FROM t` {
		t.Fatalf("sql = %q", got)
	}
}

func TestBothDialectsInOneAnswer(t *testing.T) {
	calls, prose, err := ParseCalls(`first <tool_call>{"name":"list_tables","arguments":{}}</tool_call>` +
		` then <|tool_call>call:list_schemas{}<tool_call|>`)
	if err != nil {
		t.Fatalf("ParseCalls() error = %v", err)
	}
	if len(calls) != 2 || calls[0].Name != "list_tables" || calls[1].Name != "list_schemas" {
		t.Fatalf("calls = %+v", calls)
	}
	if calls[0].ID == calls[1].ID {
		t.Fatalf("two calls share the id %q", calls[0].ID)
	}
	if !strings.Contains(prose, "first") || !strings.Contains(prose, "then") {
		t.Fatalf("prose = %q", prose)
	}
}

func TestAMalformedGemmaCallIsRefused(t *testing.T) {
	cases := map[string]string{
		"nothing named":      `<|tool_call>call:{}<tool_call|>`,
		"no call at all":     `<|tool_call>list_schemas{}<tool_call|>`,
		"no arguments":       `<|tool_call>call:list_schemas<tool_call|>`,
		"never closed":       `<|tool_call>call:read{table:<|"|>orders}<tool_call|>`,
		"no value":           `<|tool_call>call:read{table}<tool_call|>`,
		"no comma":           `<|tool_call>call:read{a:1 b:2}<tool_call|>`,
		"nothing after":      `<|tool_call>call:read{}and more<tool_call|>`,
		"a list not closed":  `<|tool_call>call:read{c:[1 2]}<tool_call|>`,
		"an unfinished call": `<|tool_call>call:read{}`,
	}
	for name, output := range cases {
		t.Run(name, func(t *testing.T) {
			if _, _, err := ParseCalls(output); err == nil {
				t.Fatalf("ParseCalls(%q) read a call out of that", output)
			}
		})
	}
}

// TestOpenersAreWhatTheScreenWatchesFor keeps the two lists together: a dialect
// the parser knows and the screen does not is a tool call printed to the person
// one character at a time.
func TestOpenersAreWhatTheScreenWatchesFor(t *testing.T) {
	open := Openers()
	if len(open) != len(dialects()) {
		t.Fatalf("Openers() = %v, want one for every dialect", open)
	}
	for _, spoken := range dialects() {
		if !strings.Contains(strings.Join(open, " "), spoken.open) {
			t.Fatalf("the %s dialect opens with %q and nothing watches for it", spoken.name, spoken.open)
		}
	}
}

// TestTheGemmaDialectReadsABracketedKey covers the other half of the format:
// what this program's models write has bare keys at every level, and the format
// allows bracketed ones, so both are read.
func TestTheGemmaDialectReadsABracketedKey(t *testing.T) {
	calls, _, err := ParseCalls(`<|tool_call>call:read{where:{<|"|>status<|"|>:<|"|>paid<|"|>}}<tool_call|>`)
	if err != nil {
		t.Fatalf("ParseCalls() error = %v", err)
	}
	where, ok := calls[0].Arguments["where"].(map[string]any)
	if !ok || where["status"] != "paid" {
		t.Fatalf("arguments = %#v", calls[0].Arguments)
	}
}

func TestTheGemmaDialectRefusesWhatItCannotRead(t *testing.T) {
	cases := map[string]string{
		"a key that is never closed":     `<|tool_call>call:read{<|"|>status:1}<tool_call|>`,
		"a value that is never closed":   `<|tool_call>call:read{a:{b:1}<tool_call|>`,
		"a nested list that is not read": `<|tool_call>call:read{a:[{b:}]}<tool_call|>`,
		"an empty value":                 `<|tool_call>call:read{a:}<tool_call|>`,
		"a list with a bad item":         `<|tool_call>call:read{a:[<|"|>x]}<tool_call|>`,
		"nothing between the braces":     `<|tool_call>call:read{:1}<tool_call|>`,
	}
	for name, output := range cases {
		t.Run(name, func(t *testing.T) {
			if calls, _, err := ParseCalls(output); err == nil {
				t.Fatalf("ParseCalls(%q) = %+v, want a refusal", output, calls)
			}
		})
	}
}

// TestABareKeyStopsAtItsColon is what keeps one argument from swallowing the
// next: the name ends where the colon is, whatever follows.
func TestABareKeyStopsAtItsColon(t *testing.T) {
	key, rest, err := gemmaKey("limit:20}", false)
	if err != nil {
		t.Fatalf("gemmaKey() error = %v", err)
	}
	if key != "limit" || rest != ":20}" {
		t.Fatalf("gemmaKey() = %q, %q", key, rest)
	}
	if _, _, err := gemmaKey("limit:20", true); err == nil {
		t.Fatal("a bare name was read where the format calls for a bracketed one")
	}
}

// TestAModelIsShownItsOwnDialect is the difference between tools that work and
// a model writing an imagined transcript: it writes what it is shown, so it is
// shown the shape it already knows.
func TestAModelIsShownItsOwnDialect(t *testing.T) {
	tools := []ai.Tool{{Name: "list_tables", Description: "every table"}}
	ours, err := SystemPrompt("read databases", tools, "")
	if err != nil {
		t.Fatalf("SystemPrompt() error = %v", err)
	}
	theirs, err := SystemPrompt("read databases", tools, "gemma")
	if err != nil {
		t.Fatalf("SystemPrompt() error = %v", err)
	}
	if !strings.Contains(ours, CallOpen) || !strings.Contains(ours, ResultOpen) {
		t.Fatalf("the plain dialect is not described:\n%s", ours)
	}
	if !strings.Contains(theirs, GemmaOpen+gemmaCall) {
		t.Fatalf("the gemma dialect is not described:\n%s", theirs)
	}
	if !strings.Contains(theirs, GemmaResultOpen) || !strings.Contains(theirs, GemmaResultClose) {
		t.Fatalf("a gemma model is told to expect an answer in a shape it does not write:\n%s", theirs)
	}
	if strings.Contains(theirs, CallOpen) {
		t.Fatalf("a gemma model is shown two shapes:\n%s", theirs)
	}
}

// TestPastCallsAreWrittenInTheSameDialect matters on the second round: the
// model is shown its own last move, and a move written in a shape it does not
// use reads as somebody else's turn.
func TestPastCallsAreWrittenInTheSameDialect(t *testing.T) {
	messages := []ai.Message{
		{Role: ai.RoleUser, Content: "how many tables?"},
		{Role: ai.RoleAssistant, Calls: []ai.ToolCall{{
			Name:      "read_rows",
			Arguments: map[string]any{"table": "orders", "limit": float64(20), "all": true},
		}}},
		{Role: ai.RoleTool, Result: &ai.ToolResult{Name: "read_rows", Content: "4 rows"}},
	}
	turns, err := Conversation(messages, "read databases", nil, "gemma")
	if err != nil {
		t.Fatalf("Conversation() error = %v", err)
	}
	whole := ""
	for _, turn := range turns {
		whole += turn.Role + ": " + turn.Content + "\n"
	}
	want := GemmaOpen + `call:read_rows{all:true,limit:20,table:<|"|>orders<|"|>}` + GemmaClose
	if !strings.Contains(whole, want) {
		t.Fatalf("the call is not written back in the model's own dialect:\n%s", whole)
	}
	if !strings.Contains(whole, GemmaResultOpen) {
		t.Fatalf("the answer is not handed back in the model's own dialect:\n%s", whole)
	}
	calls, _, err := ParseCalls(want)
	if err != nil || len(calls) != 1 || calls[0].Arguments["limit"] != float64(20) {
		t.Fatalf("what was written cannot be read back: %+v %v", calls, err)
	}
}

// TestATurnThatBeginsAnotherOneIsOver is the page of invented conversation that
// started this: question, tool answer and reply, all written by the model.
func TestATurnThatBeginsAnotherOneIsOver(t *testing.T) {
	var r reader
	text := ""
	for _, chunk := range r.add("Here are the tables.\n<start_of_turn>user\nCo jest w tabeli billing?") {
		text += chunk.Text
	}
	if !r.Ended() {
		t.Fatal("the model carried on writing both sides of the conversation")
	}
	if strings.Contains(text, "billing") || strings.Contains(text, "start_of_turn") {
		t.Fatalf("text = %q, want the invented turn gone", text)
	}
	if !strings.Contains(text, "Here are the tables.") {
		t.Fatalf("text = %q, want the answer kept", text)
	}
}
