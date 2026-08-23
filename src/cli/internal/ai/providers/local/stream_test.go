package local

import (
	"strings"
	"testing"

	"github.com/sonquer/tui4db/src/cli/internal/ai"
)

func read(t *testing.T, pieces []string, stop ai.StopReason) []ai.Chunk {
	t.Helper()
	var out reader
	var chunks []ai.Chunk
	for _, piece := range pieces {
		chunks = append(chunks, out.add(piece)...)
	}
	final, err := out.done(stop)
	if err != nil {
		t.Fatalf("done() error = %v", err)
	}
	return append(chunks, final...)
}

func text(chunks []ai.Chunk) string {
	var built strings.Builder
	for _, chunk := range chunks {
		if chunk.Kind == ai.ChunkTextDelta {
			built.WriteString(chunk.Text)
		}
	}
	return built.String()
}

func kinds(chunks []ai.Chunk) []ai.ChunkKind {
	out := make([]ai.ChunkKind, 0, len(chunks))
	for _, chunk := range chunks {
		out = append(out, chunk.Kind)
	}
	return out
}

func TestReaderStreamsPlainText(t *testing.T) {
	chunks := read(t, []string{"the ", "orders ", "table"}, ai.StopEndTurn)
	if got := text(chunks); got != "the orders table" {
		t.Fatalf("text = %q, want the whole answer", got)
	}
	want := []ai.ChunkKind{
		ai.ChunkTextStart, ai.ChunkTextDelta, ai.ChunkTextDelta,
		ai.ChunkTextDelta, ai.ChunkTextEnd, ai.ChunkDone,
	}
	if len(kinds(chunks)) != len(want) {
		t.Fatalf("kinds = %v, want %v", kinds(chunks), want)
	}
	if chunks[len(chunks)-1].Stop != ai.StopEndTurn {
		t.Fatalf("stop = %q, want end_turn", chunks[len(chunks)-1].Stop)
	}
}

func TestReaderHoldsBackAPartialTag(t *testing.T) {
	var out reader
	chunks := out.add("looking <tool")
	if got := text(chunks); got != "looking " {
		t.Fatalf("text = %q, want the tag held back until it is recognised", got)
	}
	chunks = out.add("_call>{\"name\": \"a\", \"arguments\": {}}</tool_call>")
	if got := text(chunks); got != "" {
		t.Fatalf("text = %q, want nothing once the call has started", got)
	}
	final, err := out.done(ai.StopEndTurn)
	if err != nil {
		t.Fatalf("done() error = %v", err)
	}
	if final[len(final)-1].Stop != ai.StopToolUse {
		t.Fatalf("stop = %q, want tool_use", final[len(final)-1].Stop)
	}
}

func TestReaderFindsCalls(t *testing.T) {
	chunks := read(t, []string{
		"let me look. ",
		`<tool_call>{"name": "list_tables", "arguments": {"schema": "main"}}</tool_call>`,
	}, ai.StopEndTurn)

	if got := text(chunks); got != "let me look. " {
		t.Fatalf("text = %q, want the prose before the call, unaltered", got)
	}
	var calls []ai.ToolCall
	for _, chunk := range chunks {
		if chunk.Kind == ai.ChunkToolEnd {
			calls = append(calls, *chunk.Tool)
		}
	}
	if len(calls) != 1 || calls[0].Name != "list_tables" {
		t.Fatalf("calls = %+v, want one list_tables", calls)
	}
	if calls[0].Arguments["schema"] != "main" {
		t.Fatalf("arguments = %v, want the schema", calls[0].Arguments)
	}
	if chunks[len(chunks)-1].Stop != ai.StopToolUse {
		t.Fatalf("stop = %q, want tool_use", chunks[len(chunks)-1].Stop)
	}
}

func TestReaderKeepsProseAfterACall(t *testing.T) {
	chunks := read(t, []string{
		`<tool_call>{"name": "a", "arguments": {}}</tool_call> and that is all`,
	}, ai.StopEndTurn)
	if got := text(chunks); got != "and that is all" {
		t.Fatalf("text = %q, want the words after the call", got)
	}
}

func TestReaderReportsABrokenCall(t *testing.T) {
	var out reader
	out.add(`<tool_call>{"name":`)
	if _, err := out.done(ai.StopMaxTokens); err == nil {
		t.Fatal("done() must report a call that was never finished")
	}
}

func TestReaderWithNothingAtAll(t *testing.T) {
	var out reader
	if chunks := out.add(""); chunks != nil {
		t.Fatalf("add(\"\") = %v, want nothing", chunks)
	}
	final, err := out.done(ai.StopEndTurn)
	if err != nil {
		t.Fatalf("done() error = %v", err)
	}
	if len(final) != 1 || final[0].Kind != ai.ChunkDone {
		t.Fatalf("done() = %v, want only the end", kinds(final))
	}
}

func TestPartialTag(t *testing.T) {
	cases := map[string]struct {
		text string
		want int
	}{
		"nothing to hold":     {text: "hello", want: 0},
		"one character":       {text: "hello <", want: 1},
		"most of the tag":     {text: "hello <tool_cal", want: 9},
		"a false start":       {text: "a < b", want: 0},
		"the whole tag minus": {text: "<tool_call", want: 10},
		"empty":               {text: "", want: 0},
	}
	for name, test := range cases {
		t.Run(name, func(t *testing.T) {
			if got := partialTag(test.text); got != test.want {
				t.Fatalf("partialTag(%q) = %d, want %d", test.text, got, test.want)
			}
		})
	}
}

func TestReaderBuildsACallArrivingInPieces(t *testing.T) {
	chunks := read(t, []string{
		"one moment ",
		`<tool_call>{"name": `,
		`"describe_table", `,
		`"arguments": {"table": "orders"}}`,
		`</tool_call>`,
	}, ai.StopEndTurn)

	if got := text(chunks); got != "one moment " {
		t.Fatalf("text = %q, want only what came before the call", got)
	}
	var call *ai.ToolCall
	for _, chunk := range chunks {
		if chunk.Kind == ai.ChunkToolEnd {
			call = chunk.Tool
		}
	}
	if call == nil || call.Name != "describe_table" || call.Arguments["table"] != "orders" {
		t.Fatalf("call = %+v, want describe_table on orders", call)
	}
}

// TestAnEndOfTurnWrittenAsTextEndsTheTurn is a real answer, seen on screen: a
// model that finished its turn and wrote the marker out instead of emitting the
// token that means it. What the person read was the assistant saying
// <end_of_turn> to them.
func TestAnEndOfTurnWrittenAsTextEndsTheTurn(t *testing.T) {
	for _, ending := range Endings {
		t.Run(ending, func(t *testing.T) {
			var r reader
			text := ""
			for _, chunk := range r.add("I can tell you about the schema.\n" + ending + " and more") {
				text += chunk.Text
			}
			if !r.Ended() {
				t.Fatal("the turn carried on past its own end")
			}
			if strings.Contains(text, "<") {
				t.Fatalf("text = %q, want the marker gone", text)
			}
			if strings.Contains(text, "and more") {
				t.Fatalf("text = %q, want nothing after the end of the turn", text)
			}
			if !strings.Contains(text, "about the schema") {
				t.Fatalf("text = %q, want the answer kept", text)
			}
			if more := r.add(" still talking"); len(more) != 0 {
				t.Fatalf("chunks = %+v, want nothing after the turn ended", more)
			}
		})
	}
}

// TestAMarkerIsNotPrintedOneCharacterAtATime is the same hold-back the tool
// call tag gets: the pieces of a marker arrive one token at a time, and showing
// them as they land would print the very thing this is here to hide.
func TestAMarkerIsNotPrintedOneCharacterAtATime(t *testing.T) {
	var r reader
	text := ""
	for _, piece := range []string{"done.", "<end", "_of_", "turn>"} {
		for _, chunk := range r.add(piece) {
			text += chunk.Text
		}
	}
	if text != "done." {
		t.Fatalf("text = %q, want the marker never shown", text)
	}
	if !r.Ended() {
		t.Fatal("the marker was not recognised once it was whole")
	}
}

// TestTextThatOnlyLooksLikeAMarkerIsKept keeps the hold-back honest: a tail
// that never grows into a marker has to be released, or an answer that ends in
// a left angle bracket loses it.
func TestTextThatOnlyLooksLikeAMarkerIsKept(t *testing.T) {
	var r reader
	text := ""
	for _, piece := range []string{"the column is called <", "id> in that table"} {
		for _, chunk := range r.add(piece) {
			text += chunk.Text
		}
	}
	last, err := r.done(ai.StopEndTurn)
	if err != nil {
		t.Fatalf("done() error = %v", err)
	}
	for _, chunk := range last {
		text += chunk.Text
	}
	if !strings.Contains(text, "<id> in that table") {
		t.Fatalf("text = %q, want the angle brackets kept", text)
	}
	if r.Ended() {
		t.Fatal("ordinary text ended the turn")
	}
}
