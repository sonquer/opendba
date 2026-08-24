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

// TestWhatIsHeldBehindACallIsCutToo is the leak that survived the first fix:
// from the moment a tool call starts nothing is shown until the turn is over,
// and that held text went out whole — the call, the model's own end of turn
// marker, and the conversation it imagined having next.
func TestWhatIsHeldBehindACallIsCutToo(t *testing.T) {
	var r reader
	text, calls := "", 0
	for _, chunk := range r.add("I will look.") {
		text += chunk.Text
	}
	r.add(`<|tool_call>call:list_schemas{}<tool_call|>` + "\n<end_of_turn>\n" +
		"<start_of_turn>model\nThe database has four tables.<end_of_turn>")

	chunks, err := r.done(ai.StopEndTurn)
	if err != nil {
		t.Fatalf("done() error = %v", err)
	}
	for _, chunk := range chunks {
		text += chunk.Text
		if chunk.Kind == ai.ChunkToolStart {
			calls++
		}
	}
	if calls != 1 {
		t.Fatalf("calls = %d, want the one the model actually made", calls)
	}
	for _, gone := range []string{"end_of_turn", "start_of_turn", "four tables"} {
		if strings.Contains(text, gone) {
			t.Fatalf("text holds %q:\n%s", gone, text)
		}
	}
	if !strings.Contains(text, "I will look.") {
		t.Fatalf("text = %q, want what the model said before the call", text)
	}
	if !r.Ended() {
		t.Fatal("the turn is not marked as over")
	}
}

// reasoning is what the model said to itself, the way text is what it said to
// the person.
func reasoning(chunks []ai.Chunk) string {
	var built strings.Builder
	for _, chunk := range chunks {
		if chunk.Kind == ai.ChunkReasoningDelta {
			built.WriteString(chunk.Text)
		}
	}
	return built.String()
}

// letters feeds an answer one byte at a time, which is closer to what an engine
// does than handing over whole words is. A marker split down the middle is the
// thing that breaks a reader, and the only way to be sure it does not is to
// split every one of them.
func letters(answer string) []string {
	pieces := make([]string, 0, len(answer))
	for i := range answer {
		pieces = append(pieces, answer[i:i+1])
	}
	return pieces
}

// bothWays reads an answer whole and then a byte at a time, and insists the two
// agree. Everything a reader can get wrong about a marker it gets wrong in only
// one of the two.
func bothWays(t *testing.T, answer string, stop ai.StopReason) []ai.Chunk {
	t.Helper()
	whole := read(t, []string{answer}, stop)
	split := read(t, letters(answer), stop)
	if text(whole) != text(split) {
		t.Fatalf("read whole the answer is %q; read a byte at a time it is %q",
			text(whole), text(split))
	}
	if reasoning(whole) != reasoning(split) {
		t.Fatalf("read whole the thinking is %q; read a byte at a time it is %q",
			reasoning(whole), reasoning(split))
	}
	return whole
}

// A model that thinks out loud has its thinking kept apart from its answer,
// and neither bracket reaches the person. This is the answer that prompted the
// work, copied off the screen it was drawn on.
func TestAModelsThinkingIsNotItsAnswer(t *testing.T) {
	answer := GemmaThinkOpen + "thought\n" +
		"The user is asking about \"idle indexes\", which currently reads 5.1 MiB.\n\n" +
		"I will use the health_findings tool." + GemmaThinkClose +
		"The warning indicates that 111 indexes have never been used."

	chunks := bothWays(t, answer, ai.StopEndTurn)
	if got := text(chunks); got != "The warning indicates that 111 indexes have never been used." {
		t.Errorf("the answer is %q", got)
	}
	said := reasoning(chunks)
	if !strings.Contains(said, "idle indexes") || !strings.Contains(said, "health_findings tool") {
		t.Errorf("the thinking is %q", said)
	}
	if strings.HasPrefix(said, "thought") {
		t.Errorf("the name of the channel is not the first thing thought: %q", said)
	}
	for _, marker := range Thinking() {
		if strings.Contains(text(chunks)+said, marker) {
			t.Errorf("%q reached the person", marker)
		}
	}
}

// The blocks are bracketed the way every other provider brackets them.
func TestThinkingIsBracketedLikeEverythingElse(t *testing.T) {
	chunks := read(t, []string{GemmaThinkOpen + "thought\nwhy" + GemmaThinkClose + "because"},
		ai.StopEndTurn)
	want := []ai.ChunkKind{
		ai.ChunkReasoningStart, ai.ChunkReasoningDelta, ai.ChunkReasoningEnd,
		ai.ChunkTextStart, ai.ChunkTextDelta, ai.ChunkTextEnd, ai.ChunkDone,
	}
	if got := kinds(chunks); !sameKinds(got, want) {
		t.Errorf("kinds = %v, want %v", got, want)
	}
}

func sameKinds(got, want []ai.ChunkKind) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

// Thinking that is never closed is folded away rather than printed. The model
// stopped mid-thought; what it was working through is still not the answer, and
// printing it with a bracket hanging off it is the failure this whole change is
// about.
func TestThinkingThatIsNeverClosedIsStillThinking(t *testing.T) {
	chunks := bothWays(t, "here goes "+GemmaThinkOpen+"thought\nstill working",
		ai.StopEndTurn)
	if got := text(chunks); got != "here goes " {
		t.Errorf("the answer is %q, only what was said before the thinking", got)
	}
	if got := reasoning(chunks); got != "still working" {
		t.Errorf("the thinking is %q", got)
	}
}

// A closing bracket nobody opened is not a licence to hide the answer.
func TestAStrayClosingBracketDoesNotHideAnything(t *testing.T) {
	chunks := bothWays(t, "the answer"+GemmaThinkClose+" continues", ai.StopEndTurn)
	if got := text(chunks); !strings.Contains(got, "the answer") ||
		!strings.Contains(got, "continues") {
		t.Errorf("the answer is %q, and nothing was opened to close", got)
	}
	if got := reasoning(chunks); got != "" {
		t.Errorf("nothing was thought, got %q", got)
	}
}

// A tool call written inside the thinking is still a tool call.
func TestAToolCallInsideTheThinkingIsStillACall(t *testing.T) {
	answer := GemmaThinkOpen + "thought\nI will look." + GemmaThinkClose +
		GemmaOpen + "call:list_schemas{}" + GemmaClose
	chunks := read(t, []string{answer}, ai.StopEndTurn)
	calls := 0
	for _, chunk := range chunks {
		if chunk.Kind == ai.ChunkToolEnd {
			calls++
		}
	}
	if calls != 1 {
		t.Errorf("calls = %d, want 1", calls)
	}
	if got := reasoning(chunks); !strings.Contains(got, "I will look") {
		t.Errorf("the thinking is %q", got)
	}
}

// An answer with no thinking in it reads exactly as it did before any of this.
func TestAnAnswerWithoutThinkingIsUntouched(t *testing.T) {
	answer := "the orders table has 12 columns"
	chunks := bothWays(t, answer, ai.StopEndTurn)
	if got := text(chunks); got != answer {
		t.Errorf("text = %q, want %q", got, answer)
	}
	if got := reasoning(chunks); got != "" {
		t.Errorf("nothing was thought, got %q", got)
	}
	for _, kind := range kinds(chunks) {
		if kind == ai.ChunkReasoningStart || kind == ai.ChunkReasoningDelta {
			t.Errorf("kinds = %v, no reasoning block belongs here", kinds(chunks))
			break
		}
	}
}

// The name of the channel is dropped however it is written.
func TestTheNameOfTheChannelIsDropped(t *testing.T) {
	for name, opened := range map[string]string{
		"a named channel":     GemmaThinkOpen + "thought\nwhy",
		"another name":        GemmaThinkOpen + "analysis why",
		"no name at all":      GemmaThinkOpen + "why",
		"a name and a spacer": GemmaThinkOpen + "thought \twhy",
	} {
		t.Run(name, func(t *testing.T) {
			chunks := read(t, []string{opened + GemmaThinkClose + "so"}, ai.StopEndTurn)
			if got := reasoning(chunks); !strings.HasPrefix(got, "why") {
				t.Errorf("the thinking is %q, want it to begin at the thought", got)
			}
			if got := text(chunks); got != "so" {
				t.Errorf("the answer is %q", got)
			}
		})
	}
}

// Every marker the reader watches for is one it will not print half of.
func TestTheThinkingMarkersAreWhatTheScreenWatchesFor(t *testing.T) {
	if len(Thinking()) == 0 {
		t.Fatal("a family that thinks out loud must say so")
	}
	for _, marker := range Thinking() {
		t.Run(marker, func(t *testing.T) {
			if partialTag(marker[:len(marker)-1]) == 0 {
				t.Errorf("%q would be drawn one character at a time", marker)
			}
		})
	}
}
