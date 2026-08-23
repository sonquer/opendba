package agent

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/sonquer/tui4db/src/cli/internal/ai"
)

type scripted struct {
	turns   [][]ai.Chunk
	err     error
	local   bool
	asked   []ai.Request
	at      int
	failing []error
}

func (s *scripted) Capabilities() ai.Capabilities {
	return ai.Capabilities{Tools: true, Streaming: true, Local: s.local}
}

func (s *scripted) Chat(_ context.Context, req ai.Request) (ai.Stream, error) {
	s.asked = append(s.asked, req)
	if s.at < len(s.failing) && s.failing[s.at] != nil {
		err := s.failing[s.at]
		s.at++
		return nil, err
	}
	if s.err != nil {
		return nil, s.err
	}
	if s.at >= len(s.turns) {
		return nil, errors.New("the model was asked more times than the test scripted")
	}
	chunks := s.turns[s.at]
	s.at++
	return &replay{chunks: chunks}, nil
}

type replay struct {
	chunks []ai.Chunk
	at     int
	closed int
}

func (r *replay) Next() (ai.Chunk, error) {
	if r.at >= len(r.chunks) {
		return ai.Chunk{}, io.EOF
	}
	chunk := r.chunks[r.at]
	r.at++
	return chunk, nil
}

func (r *replay) Close() error {
	r.closed++
	return nil
}

type fakeTools struct {
	definitions []ai.Tool
	results     map[string]ai.ToolResult
	called      []ai.ToolCall
}

func (f *fakeTools) Definitions() []ai.Tool { return f.definitions }

func (f *fakeTools) Call(_ context.Context, call ai.ToolCall) ai.ToolResult {
	f.called = append(f.called, call)
	if result, ok := f.results[call.Name]; ok {
		result.ID, result.Name = call.ID, call.Name
		return result
	}
	return ai.ToolResult{ID: call.ID, Name: call.Name, Content: "nothing to show"}
}

type fakeConsent struct {
	asked  []Outbound
	refuse error
}

func (f *fakeConsent) Allow(_ context.Context, outbound Outbound) error {
	f.asked = append(f.asked, outbound)
	return f.refuse
}

func text(chunks ...string) []ai.Chunk {
	built := []ai.Chunk{{Kind: ai.ChunkTextStart}}
	for _, chunk := range chunks {
		built = append(built, ai.Chunk{Kind: ai.ChunkTextDelta, Text: chunk})
	}
	return append(built, ai.Chunk{Kind: ai.ChunkTextEnd},
		ai.Chunk{Kind: ai.ChunkDone, Stop: ai.StopEndTurn, Usage: &ai.Usage{Input: 10, Output: 5}})
}

func calling(name string, arguments map[string]any) []ai.Chunk {
	call := ai.ToolCall{ID: "call_1", Name: name, Arguments: arguments}
	return []ai.Chunk{
		{Kind: ai.ChunkToolStart},
		{Kind: ai.ChunkToolEnd, Tool: &call},
		{Kind: ai.ChunkDone, Stop: ai.StopToolUse},
	}
}

func ask(t *testing.T, agent *Agent, question string) ([]Event, error) {
	t.Helper()
	out := make(chan Event, 64)
	err := agent.Ask(context.Background(), question, out)
	close(out)
	var events []Event
	for event := range out {
		events = append(events, event)
	}
	return events, err
}

func spoken(events []Event) string {
	var built strings.Builder
	for _, event := range events {
		if event.Kind == EventText {
			built.WriteString(event.Text)
		}
	}
	return built.String()
}

func agentFor(client ai.Client, tools Tools, consent Consent) *Agent {
	return New(client, tools, consent, ai.Instance{Name: "claude", Model: "claude-sonnet-5"}, "you read databases")
}

func TestAskAnswersWithoutTools(t *testing.T) {
	client := &scripted{turns: [][]ai.Chunk{text("orders ", "is the biggest")}}
	tools := &fakeTools{}
	agent := agentFor(client, tools, &fakeConsent{})

	events, err := ask(t, agent, "which table is biggest?")
	if err != nil {
		t.Fatalf("Ask() error = %v", err)
	}
	if got := spoken(events); got != "orders is the biggest" {
		t.Fatalf("text = %q", got)
	}
	last := events[len(events)-1]
	if last.Kind != EventDone || last.Stop != ai.StopEndTurn || last.Usage == nil {
		t.Fatalf("last event = %+v", last)
	}
	if len(tools.called) != 0 {
		t.Fatalf("tools were called: %+v", tools.called)
	}
	if len(agent.Messages()) != 2 {
		t.Fatalf("the conversation holds %d messages, want the question and the answer", len(agent.Messages()))
	}
}

func TestAskRunsAToolAndAsksAgain(t *testing.T) {
	client := &scripted{turns: [][]ai.Chunk{
		calling("list_tables", map[string]any{"schema": "main"}),
		text("orders and users"),
	}}
	tools := &fakeTools{results: map[string]ai.ToolResult{
		"list_tables": {Content: "orders\nusers"},
	}}
	agent := agentFor(client, tools, &fakeConsent{})

	events, err := ask(t, agent, "what is in there?")
	if err != nil {
		t.Fatalf("Ask() error = %v", err)
	}
	if got := spoken(events); got != "orders and users" {
		t.Fatalf("text = %q", got)
	}
	if len(tools.called) != 1 || tools.called[0].Name != "list_tables" {
		t.Fatalf("tools called = %+v", tools.called)
	}
	var sawCall, sawResult bool
	for _, event := range events {
		switch event.Kind {
		case EventCall:
			sawCall = true
		case EventResult:
			sawResult = true
		}
	}
	if !sawCall || !sawResult {
		t.Fatal("the caller was not told about the call and its result")
	}
	if len(client.asked) != 2 {
		t.Fatalf("the model was asked %d times, want twice", len(client.asked))
	}
	if len(client.asked[1].Messages) != 3 {
		t.Fatalf("the second request carried %d messages, want the question, the call and the result", len(client.asked[1].Messages))
	}
}

func TestAskStopsLooping(t *testing.T) {
	turns := make([][]ai.Chunk, DefaultRounds)
	for i := range turns {
		turns[i] = calling("list_tables", nil)
	}
	client := &scripted{turns: turns}
	agent := agentFor(client, &fakeTools{}, &fakeConsent{})

	events, err := ask(t, agent, "go round for ever")
	if err != nil {
		t.Fatalf("Ask() error = %v", err)
	}
	if len(client.asked) != DefaultRounds {
		t.Fatalf("the model was asked %d times, want it stopped at %d", len(client.asked), DefaultRounds)
	}
	last := events[len(events)-1]
	if last.Kind != EventDone || last.Stop != ai.StopMaxTokens {
		t.Fatalf("last event = %+v, want the turn stopped", last)
	}
}

func TestConsentIsAskedOncePerClass(t *testing.T) {
	client := &scripted{turns: [][]ai.Chunk{
		calling("list_tables", nil),
		calling("run_select", map[string]any{"statement": "SELECT 1"}),
		text("done"),
	}}
	tools := &fakeTools{results: map[string]ai.ToolResult{
		"list_tables": {Content: "orders"},
		"run_select":  {Content: "1"},
	}}
	consent := &fakeConsent{}
	agent := agentFor(client, tools, consent)

	if _, err := ask(t, agent, "have a look"); err != nil {
		t.Fatalf("Ask() error = %v", err)
	}
	if len(consent.asked) != 3 {
		t.Fatalf("consent was asked %d times, want once per new class", len(consent.asked))
	}
	if len(consent.asked[0].Classes) != 1 || consent.asked[0].Classes[0] != ClassQuestion {
		t.Fatalf("the first ask = %+v, want only the question", consent.asked[0])
	}
	if consent.asked[1].Classes[0] != ClassSchema {
		t.Fatalf("the second ask = %+v, want the shape of the database", consent.asked[1])
	}
	if consent.asked[2].Classes[0] != ClassRows {
		t.Fatalf("the third ask = %+v, want the rows", consent.asked[2])
	}
	if consent.asked[2].Instance != "claude" || consent.asked[2].Model != "claude-sonnet-5" {
		t.Fatalf("the ask did not say where it was going: %+v", consent.asked[2])
	}
	if consent.asked[2].Bytes == 0 {
		t.Fatal("the ask did not say how much would be sent")
	}
}

func TestConsentCanRefuse(t *testing.T) {
	refused := errors.New("not this time")
	client := &scripted{turns: [][]ai.Chunk{text("never reached")}}
	agent := agentFor(client, &fakeTools{}, &fakeConsent{refuse: refused})

	_, err := ask(t, agent, "which table?")
	if !errors.Is(err, refused) {
		t.Fatalf("Ask() error = %v, want %v", err, refused)
	}
	if len(client.asked) != 0 {
		t.Fatal("a refused turn reached the model anyway")
	}
}

func TestALocalModelIsNotGated(t *testing.T) {
	client := &scripted{local: true, turns: [][]ai.Chunk{text("here")}}
	consent := &fakeConsent{}
	agent := agentFor(client, &fakeTools{}, consent)

	if _, err := ask(t, agent, "which table?"); err != nil {
		t.Fatalf("Ask() error = %v", err)
	}
	if len(consent.asked) != 0 {
		t.Fatal("a model running on this machine was gated as though it were not")
	}
}

func TestNoConsentAtAll(t *testing.T) {
	client := &scripted{turns: [][]ai.Chunk{text("here")}}
	agent := agentFor(client, &fakeTools{}, nil)
	if _, err := ask(t, agent, "which table?"); err != nil {
		t.Fatalf("Ask() error = %v", err)
	}
}

func TestAskRefusesNothing(t *testing.T) {
	agent := agentFor(&scripted{}, &fakeTools{}, &fakeConsent{})
	if _, err := ask(t, agent, "   "); err == nil {
		t.Fatal("Ask() must refuse an empty question")
	}
}

func TestAConversationThatNoLongerFitsIsCompacted(t *testing.T) {
	overflow := ai.Failure(ai.ReasonContextOverflow, "claude", "maximum context length exceeded", nil)
	client := &scripted{
		failing: []error{nil, overflow, nil},
		turns: [][]ai.Chunk{
			calling("list_tables", nil),
			nil,
			text("orders"),
		},
	}
	tools := &fakeTools{results: map[string]ai.ToolResult{"list_tables": {Content: strings.Repeat("x", 500)}}}
	agent := agentFor(client, tools, &fakeConsent{})

	events, err := ask(t, agent, "what is in there?")
	if err != nil {
		t.Fatalf("Ask() error = %v", err)
	}
	if got := spoken(events); got != "orders" {
		t.Fatalf("text = %q", got)
	}
	var dropped bool
	for _, message := range agent.Messages() {
		if message.Result != nil && message.Result.Content == droppedResult {
			dropped = true
		}
	}
	if !dropped {
		t.Fatal("nothing was dropped to make room")
	}
}

func TestAConversationWithNothingLeftToDropGivesUp(t *testing.T) {
	overflow := ai.Failure(ai.ReasonContextOverflow, "claude", "prompt is too long", nil)
	client := &scripted{failing: []error{overflow}, turns: [][]ai.Chunk{nil}}
	agent := agentFor(client, &fakeTools{}, &fakeConsent{})

	if _, err := ask(t, agent, "a question with no results behind it"); err == nil {
		t.Fatal("Ask() must report an overflow it cannot make room for")
	}
}

func TestOtherFailuresAreNotRetried(t *testing.T) {
	denied := ai.Failure(ai.ReasonAuth, "claude", "invalid key", nil)
	client := &scripted{failing: []error{denied}, turns: [][]ai.Chunk{nil}}
	agent := agentFor(client, &fakeTools{}, &fakeConsent{})

	_, err := ask(t, agent, "which table?")
	if reason, _ := ai.ReasonOf(err); reason != ai.ReasonAuth {
		t.Fatalf("Ask() error = %v, want the refusal passed on", err)
	}
	if len(client.asked) != 1 {
		t.Fatalf("the model was asked %d times, want once", len(client.asked))
	}
}

func TestAStreamThatFailsPartWayThrough(t *testing.T) {
	broken := errors.New("connection reset")
	client := &scripted{turns: [][]ai.Chunk{{
		{Kind: ai.ChunkTextStart},
		{Kind: ai.ChunkTextDelta, Text: "half an ans"},
	}}}
	agent := agentFor(client, &fakeTools{}, &fakeConsent{})
	agent.client = &failingStream{inner: client, err: broken}

	if _, err := ask(t, agent, "which table?"); !errors.Is(err, broken) {
		t.Fatalf("Ask() error = %v, want %v", err, broken)
	}
}

type failingStream struct {
	inner *scripted
	err   error
}

func (f *failingStream) Capabilities() ai.Capabilities { return f.inner.Capabilities() }

func (f *failingStream) Chat(ctx context.Context, req ai.Request) (ai.Stream, error) {
	stream, err := f.inner.Chat(ctx, req)
	if err != nil {
		return nil, err
	}
	return &brokenAfter{inner: stream, err: f.err}, nil
}

type brokenAfter struct {
	inner ai.Stream
	err   error
	sent  int
}

func (b *brokenAfter) Next() (ai.Chunk, error) {
	if b.sent >= 2 {
		return ai.Chunk{}, b.err
	}
	b.sent++
	return b.inner.Next()
}

func (b *brokenAfter) Close() error { return b.inner.Close() }

func TestReasoningReachesTheCaller(t *testing.T) {
	client := &scripted{turns: [][]ai.Chunk{{
		{Kind: ai.ChunkReasoningStart},
		{Kind: ai.ChunkReasoningDelta, Text: "they mean the largest"},
		{Kind: ai.ChunkReasoningEnd},
		{Kind: ai.ChunkTextStart},
		{Kind: ai.ChunkTextDelta, Text: "orders"},
		{Kind: ai.ChunkDone, Stop: ai.StopEndTurn},
	}}}
	agent := agentFor(client, &fakeTools{}, &fakeConsent{})

	events, err := ask(t, agent, "which?")
	if err != nil {
		t.Fatalf("Ask() error = %v", err)
	}
	var thought string
	for _, event := range events {
		if event.Kind == EventReasoning {
			thought += event.Text
		}
	}
	if thought != "they mean the largest" {
		t.Fatalf("reasoning = %q", thought)
	}
	if agent.Messages()[1].Reasoning != thought {
		t.Fatal("the reasoning was not kept with the answer")
	}
}

func TestTheRequestCarriesTheToolsAndTheSystemPrompt(t *testing.T) {
	client := &scripted{turns: [][]ai.Chunk{text("here")}}
	tools := &fakeTools{definitions: []ai.Tool{{Name: "list_tables"}}}
	agent := agentFor(client, tools, &fakeConsent{})

	if _, err := ask(t, agent, "which?"); err != nil {
		t.Fatalf("Ask() error = %v", err)
	}
	request := client.asked[0]
	if request.System != "you read databases" {
		t.Fatalf("system = %q", request.System)
	}
	if len(request.Tools) != 1 || request.Tools[0].Name != "list_tables" {
		t.Fatalf("tools = %+v", request.Tools)
	}
	if request.Model != "claude-sonnet-5" {
		t.Fatalf("model = %q", request.Model)
	}
}

func TestACancelledTurnStops(t *testing.T) {
	client := &scripted{turns: [][]ai.Chunk{text("one", "two", "three")}}
	agent := agentFor(client, &fakeTools{}, &fakeConsent{})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	out := make(chan Event, 1)
	if err := agent.Ask(ctx, "which?", out); !errors.Is(err, context.Canceled) {
		t.Fatalf("Ask() error = %v, want context.Canceled", err)
	}
}

type cancelling struct {
	cancel context.CancelFunc
	calls  int
}

func (c *cancelling) Definitions() []ai.Tool { return nil }

func (c *cancelling) Call(context.Context, ai.ToolCall) ai.ToolResult {
	c.calls++
	c.cancel()
	return ai.ToolResult{Content: "done"}
}

func TestWorkStopsWhenTheTurnIsCancelled(t *testing.T) {
	first := ai.ToolCall{ID: "call_1", Name: "list_tables"}
	second := ai.ToolCall{ID: "call_2", Name: "list_indexes"}
	client := &scripted{turns: [][]ai.Chunk{{
		{Kind: ai.ChunkToolStart},
		{Kind: ai.ChunkToolEnd, Tool: &first},
		{Kind: ai.ChunkToolStart},
		{Kind: ai.ChunkToolEnd, Tool: &second},
		{Kind: ai.ChunkDone, Stop: ai.StopToolUse},
	}}}
	ctx, cancel := context.WithCancel(context.Background())
	tools := &cancelling{cancel: cancel}
	agent := agentFor(client, tools, nil)

	out := make(chan Event, 64)
	err := agent.Ask(ctx, "have a look", out)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Ask() error = %v, want context.Canceled", err)
	}
	if tools.calls != 1 {
		t.Fatalf("the tools were called %d times, want the second one abandoned", tools.calls)
	}
}

func TestWorkStopsWhenNobodyIsReading(t *testing.T) {
	call := ai.ToolCall{ID: "call_1", Name: "list_tables"}
	client := &scripted{turns: [][]ai.Chunk{{
		{Kind: ai.ChunkToolStart},
		{Kind: ai.ChunkToolEnd, Tool: &call},
		{Kind: ai.ChunkDone, Stop: ai.StopToolUse},
	}}}
	ctx, cancel := context.WithCancel(context.Background())
	agent := agentFor(client, &fakeTools{}, nil)

	out := make(chan Event)
	cancel()
	if err := agent.Ask(ctx, "have a look", out); !errors.Is(err, context.Canceled) {
		t.Fatalf("Ask() error = %v, want context.Canceled", err)
	}
}
