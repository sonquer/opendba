package app

import (
	"context"
	"errors"
	"image/color"
	"strings"
	"testing"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/sonquer/opendba/src/cli/internal/ai"
	"github.com/sonquer/opendba/src/cli/internal/ai/agent"
	"github.com/sonquer/opendba/src/cli/internal/ai/providers/local"
	"github.com/sonquer/opendba/src/cli/internal/ui"
)

// scripted is a conversation that answers from a list rather than from a model.
type scripted struct {
	events  []agent.Event
	err     error
	asked   []string
	consent agent.Consent
	allowed permission
	gated   bool

	// statement is a query the conversation puts up for approval before it
	// runs, the way the tools do.
	statement string
	ran       bool
	refused   error

	class agent.Class
	held  chan struct{}

	// warming and closed are the two things a model that runs here does around
	// a conversation: it is read into memory before the first question, and let
	// go of after the last.
	warming chan struct{}
	warmErr error
	warmed  bool
	closed  bool

	// messages is what a conversation that remembers hands back, which is what
	// keeping one and opening it later runs on.
	messages []ai.Message
}

// Messages and Resume make this a conversation that remembers, the way an agent
// is.
func (s *scripted) Messages() []ai.Message { return s.messages }

func (s *scripted) Resume(messages []ai.Message) {
	s.messages = append([]ai.Message(nil), messages...)
}

// Warm is the load, held open when a test wants to look at the screen while it
// is happening.
func (s *scripted) Warm(ctx context.Context) error {
	if s.warming != nil {
		select {
		case <-s.warming:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	s.warmed = true
	return s.warmErr
}

func (s *scripted) Close() error {
	s.closed = true
	return nil
}

func (s *scripted) Ask(ctx context.Context, question string, out chan<- agent.Event) error {
	s.asked = append(s.asked, question)
	s.messages = append(s.messages, ai.Message{Role: ai.RoleUser, Content: question})
	defer func() { s.messages = append(s.messages, s.answered()) }()
	if s.statement != "" {
		if err := s.allowed.Statement(ctx, s.statement); err != nil {
			s.refused = err
			return nil
		}
		s.ran = true
	}
	if s.gated {
		err := s.consent.Allow(ctx, agent.Outbound{
			Instance: "claude", Model: "claude-sonnet-5",
			Classes: []agent.Class{s.class}, Bytes: 2048,
		})
		if err != nil {
			return err
		}
	}
	for _, event := range s.events {
		select {
		case out <- event:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	if s.held != nil {
		select {
		case <-s.held:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return s.err
}

// answered is the message an assistant would have left behind for the events
// this conversation sent.
func (s *scripted) answered() ai.Message {
	said := ai.Message{Role: ai.RoleAssistant}
	for _, event := range s.events {
		switch event.Kind {
		case agent.EventText:
			said.Content += event.Text
		case agent.EventReasoning:
			said.Reasoning += event.Text
		case agent.EventCall:
			if event.Call != nil {
				said.Calls = append(said.Calls, *event.Call)
			}
		}
	}
	return said
}

// forgetful is a conversation that cannot hand anything back, which the screen
// has to keep working without: the interface it needs is separate from the one
// it is built on precisely so that this compiles.
type forgetful struct{ events []agent.Event }

func (f *forgetful) Warm(context.Context) error { return nil }

func (f *forgetful) Close() error { return nil }

func (f *forgetful) Ask(ctx context.Context, _ string, out chan<- agent.Event) error {
	for _, event := range f.events {
		select {
		case out <- event:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

func talking(t *testing.T, talk *scripted) Model {
	t.Helper()
	m := loaded(t, healthy()).WithAssistant("claude", func(consent permission) (conversation, error) {
		talk.consent, talk.allowed = consent, consent
		return talk, nil
	})
	opened, _ := m.show(viewAsk)
	return opened.(Model)
}

// converse drives a whole turn: every command a message produces is run and fed
// back, which is what the program does.
func converse(t *testing.T, m Model, cmd tea.Cmd) Model {
	t.Helper()
	pending := []tea.Cmd{cmd}
	for range 256 {
		if len(pending) == 0 {
			return m
		}
		next := pending[0]
		pending = pending[1:]
		if next == nil {
			continue
		}
		msg := next()
		if batch, ok := msg.(tea.BatchMsg); ok {
			pending = append(pending, batch...)
			continue
		}
		if msg == nil {
			continue
		}
		updated, produced := m.Update(msg)
		m = updated.(Model)
		pending = append(pending, produced)
	}
	t.Fatal("the conversation did not settle")
	return m
}

func said(t *testing.T, m Model) string {
	t.Helper()
	return plain(m.askBody())
}

func TestAskOpensWithTheKey(t *testing.T) {
	m := loaded(t, healthy())
	opened, _ := press(t, m, "a")
	if opened.view != viewAsk {
		t.Fatalf("view = %q, want ask", opened.view)
	}
	if !strings.Contains(said(t, opened), "ask") {
		t.Fatal("the screen does not say what it is")
	}
}

func TestAskWithNothingToAnswer(t *testing.T) {
	m := loaded(t, healthy())
	opened, _ := press(t, m, "a")

	body := said(t, opened)
	if strings.Contains(body, "settings.toml") {
		t.Fatalf("body = %q, want it to lead somewhere rather than to a text editor", body)
	}
	if !strings.Contains(body, "choose what answers") {
		t.Fatalf("body = %q, want it to say which key to press", body)
	}
	if strings.Contains(plain(opened.content()), "not configured") {
		t.Fatal("the header still carries the assistant's state, where this program says what the database is")
	}
}

func TestAskOpensTheChooserWhenNothingAnswers(t *testing.T) {
	m := loaded(t, healthy())
	opened, _ := press(t, m, "a")
	chosen, _ := press(t, opened, "enter")

	if chosen.chooser == nil {
		t.Fatal("enter with nothing configured did not open the way to configure something")
	}
	shown := strings.ToLower(plain(chosen.content()))
	if !strings.Contains(shown, "on this machine") {
		t.Fatalf("the modal does not offer what runs here:\n%s", shown)
	}
	if !strings.Contains(shown, "gemma 4 e4b") {
		t.Fatalf("the modal does not list a model:\n%s", shown)
	}
	sections := map[string]bool{}
	for _, item := range chosen.chooser.offers {
		sections[item.section] = true
	}
	for _, want := range []string{"on this machine", "anthropic", "openai", "gemini", "ollama"} {
		if !sections[want] {
			t.Fatalf("the modal has no group for %s, only %v", want, sections)
		}
	}
	if chosen.chooser.offers[0].section != "on this machine" {
		t.Fatalf("the first group is %q, want the one that needs nothing from anybody",
			chosen.chooser.offers[0].section)
	}
}

func TestAskAnswers(t *testing.T) {
	talk := &scripted{events: []agent.Event{
		{Kind: agent.EventText, Text: "orders "},
		{Kind: agent.EventText, Text: "is the biggest"},
		{Kind: agent.EventDone, Stop: ai.StopEndTurn},
	}}
	m := talking(t, talk)
	m = typeInto(t, m, "which is biggest")
	m, cmd := press(t, m, "enter")
	m = converse(t, m, cmd)

	body := said(t, m)
	if !strings.Contains(body, "which is biggest") {
		t.Fatalf("body = %q, want the question kept", body)
	}
	if !strings.Contains(body, "orders is the biggest") {
		t.Fatalf("body = %q, want the answer", body)
	}
	if len(talk.asked) != 1 || talk.asked[0] != "which is biggest" {
		t.Fatalf("asked = %v", talk.asked)
	}
	if m.talk.busy {
		t.Fatal("the screen still thinks it is working")
	}
}

func TestAskShowsWhatItLookedAt(t *testing.T) {
	call := ai.ToolCall{Name: "describe_table", Arguments: map[string]any{"table": "orders", "schema": "main"}}
	talk := &scripted{events: []agent.Event{
		{Kind: agent.EventCall, Call: &call},
		{Kind: agent.EventResult, Result: &ai.ToolResult{Name: "describe_table", Content: "id, customer"}},
		{Kind: agent.EventText, Text: "two columns"},
		{Kind: agent.EventDone, Stop: ai.StopEndTurn},
	}}
	m := talking(t, talk)
	m = typeInto(t, m, "describe orders")
	m, cmd := press(t, m, "enter")
	m = converse(t, m, cmd)

	body := said(t, m)
	if !strings.Contains(body, "describe_table(schema: main, table: orders)") {
		t.Fatalf("body = %q, want the call written out", body)
	}
	if !strings.Contains(body, "two columns") {
		t.Fatalf("body = %q, want the answer", body)
	}
}

func TestAskShowsAToolThatRefused(t *testing.T) {
	talk := &scripted{events: []agent.Event{
		{Kind: agent.EventResult, Result: &ai.ToolResult{Name: "run_select", Content: "refused: this deletes rows", Failed: true}},
		{Kind: agent.EventText, Text: "I cannot do that"},
		{Kind: agent.EventDone, Stop: ai.StopEndTurn},
	}}
	m := talking(t, talk)
	m = typeInto(t, m, "delete everything")
	m, cmd := press(t, m, "enter")
	m = converse(t, m, cmd)

	if !strings.Contains(said(t, m), "refused: this deletes rows") {
		t.Fatalf("body = %q, want the refusal shown", said(t, m))
	}
}

func TestAskReportsAFailure(t *testing.T) {
	talk := &scripted{err: errors.New("the key was refused")}
	m := talking(t, talk)
	m = typeInto(t, m, "hello")
	m, cmd := press(t, m, "enter")
	m = converse(t, m, cmd)

	if !strings.Contains(said(t, m), "the key was refused") {
		t.Fatalf("body = %q, want the failure said out loud", said(t, m))
	}
}

func TestAskAsksBeforeSending(t *testing.T) {
	talk := &scripted{
		gated: true,
		class: agent.ClassSchema,
		events: []agent.Event{
			{Kind: agent.EventText, Text: "orders"},
			{Kind: agent.EventDone, Stop: ai.StopEndTurn},
		},
	}
	m := talking(t, talk)
	m = typeInto(t, m, "which is biggest")
	m, cmd := press(t, m, "enter")

	m = converse4Approval(t, m, cmd)
	body := said(t, m)
	if !strings.Contains(body, "send this?") {
		t.Fatalf("body = %q, want the question about sending", body)
	}
	if !strings.Contains(body, string(agent.ClassSchema)) {
		t.Fatalf("body = %q, want it to name what would be sent", body)
	}
	if !strings.Contains(body, "claude-sonnet-5") {
		t.Fatalf("body = %q, want it to name where it would go", body)
	}

	allowed, cmd := press(t, m, "enter")
	allowed = converse(t, allowed, cmd)
	if !strings.Contains(said(t, allowed), "orders") {
		t.Fatalf("body = %q, want the answer once it was allowed", said(t, allowed))
	}
}

func TestAskCanRefuseToSend(t *testing.T) {
	talk := &scripted{gated: true, class: agent.ClassRows}
	m := talking(t, talk)
	m = typeInto(t, m, "show me the rows")
	m, cmd := press(t, m, "enter")
	m = converse4Approval(t, m, cmd)

	refused, cmd := press(t, m, "esc")
	refused = converse(t, refused, cmd)
	if !strings.Contains(said(t, refused), "nothing was sent") {
		t.Fatalf("body = %q, want it to say nothing left the machine", said(t, refused))
	}
}

// converse4Approval runs the turn until the question about sending is on screen.
func converse4Approval(t *testing.T, m Model, cmd tea.Cmd) Model {
	t.Helper()
	for range 16 {
		if m.talk.pending != nil {
			return m
		}
		if cmd == nil {
			t.Fatal("the turn ended without asking")
		}
		msg := cmd()
		if batch, ok := msg.(tea.BatchMsg); ok {
			var next tea.Cmd
			for _, sub := range batch {
				if sub == nil {
					continue
				}
				produced := sub()
				if produced == nil {
					continue
				}
				updated, one := m.Update(produced)
				m = updated.(Model)
				if one != nil && next == nil {
					next = one
				}
			}
			cmd = next
			continue
		}
		updated, next := m.Update(msg)
		m, cmd = updated.(Model), next
	}
	t.Fatal("the question about sending never arrived")
	return m
}

func TestAskStopsAnAnswerPartWayThrough(t *testing.T) {
	talk := &scripted{
		events: []agent.Event{{Kind: agent.EventText, Text: "half an ans"}},
		held:   make(chan struct{}),
	}
	m := talking(t, talk)
	m = typeInto(t, m, "a long question")
	m, cmd := press(t, m, "enter")
	if !m.talk.busy {
		t.Fatal("the screen does not think it is working")
	}

	m, reading := step4Ask(t, m, cmd)
	if !strings.Contains(said(t, m), "half an ans") {
		t.Fatalf("body = %q, want what arrived before the stop", said(t, m))
	}

	stopped, _ := press(t, m, "esc")
	stopped = converse(t, stopped, reading)
	if stopped.talk.busy {
		t.Fatal("the turn was not stopped")
	}
	if !strings.Contains(said(t, stopped), "stopped") {
		t.Fatalf("body = %q, want it to say the answer was stopped", said(t, stopped))
	}
	if !strings.Contains(said(t, stopped), "half an ans") {
		t.Fatalf("body = %q, want the half that did arrive kept", said(t, stopped))
	}
}

// step4Ask runs one round of a turn and hands back the command that is still
// reading it, the way the program keeps a command in flight while a key is
// pressed.
func step4Ask(t *testing.T, m Model, cmd tea.Cmd) (Model, tea.Cmd) {
	t.Helper()
	msg := cmd()
	batch, ok := msg.(tea.BatchMsg)
	if !ok {
		updated, next := m.Update(msg)
		return updated.(Model), next
	}
	var reading tea.Cmd
	for _, sub := range batch {
		if sub == nil {
			continue
		}
		produced := sub()
		if produced == nil {
			continue
		}
		if _, tick := produced.(spinner.TickMsg); tick {
			continue
		}
		updated, next := m.Update(produced)
		m, reading = updated.(Model), next
	}
	if reading == nil {
		t.Fatal("nothing was left reading the turn")
	}
	return m, reading
}

func TestAskGoesBackWhenNothingIsRunning(t *testing.T) {
	m := talking(t, &scripted{})
	back, _ := press(t, m, "esc")
	if back.view != viewDashboard {
		t.Fatalf("view = %q, want the dashboard", back.view)
	}
}

func TestAskContinuesALineThatEndsInABackslash(t *testing.T) {
	talk := &scripted{}
	m := talking(t, talk)
	m = typeInto(t, m, "one")
	m, _ = press(t, m, `\`)
	m, cmd := press(t, m, "enter")
	if cmd != nil {
		t.Fatal("a line that was continued must not be sent")
	}
	if len(talk.asked) != 0 {
		t.Fatalf("asked = %v, want nothing sent", talk.asked)
	}
	if !strings.Contains(m.talk.prompt.Value(), "\n") {
		t.Fatalf("prompt = %q, want the backslash turned into a new line", m.talk.prompt.Value())
	}
}

func TestAskSendsNothingWhenNothingWasTyped(t *testing.T) {
	talk := &scripted{}
	_, cmd := press(t, talking(t, talk), "enter")
	if cmd != nil || len(talk.asked) != 0 {
		t.Fatal("an empty question was sent")
	}
}

func TestAskIgnoresAStaleTurn(t *testing.T) {
	m := talking(t, &scripted{})
	m.talk.token = 4

	updated, _ := m.Update(askEventMsg{
		event: agent.Event{Kind: agent.EventText, Text: "from before"}, token: 1, on: m.id})
	if strings.Contains(said(t, updated.(Model)), "from before") {
		t.Fatal("a piece of an answer from a turn that was abandoned reached the screen")
	}

	answer := make(chan error, 1)
	updated, _ = m.Update(askApprovalMsg{request: approval{answer: answer}, token: 1, on: m.id})
	if err := <-answer; !errors.Is(err, errRefused) {
		t.Fatalf("a stale turn was answered with %v, want it refused", err)
	}
	if updated.(Model).talk.pending != nil {
		t.Fatal("a stale turn put a question on the screen")
	}

	updated, _ = m.Update(askEndedMsg{token: 1, on: m.id})
	if updated.(Model).talk.busy != m.talk.busy {
		t.Fatal("a stale ending changed the screen")
	}
}

func TestAskInThePalette(t *testing.T) {
	m := loaded(t, healthy())
	opened, _ := press(t, m, "/")
	if !strings.Contains(plain(opened.content()), "ask") {
		t.Fatal("the palette does not offer the conversation")
	}
}

func TestAskAboutAReadingOnTheDashboard(t *testing.T) {
	talk := &scripted{}
	m := loaded(t, healthy()).WithAssistant("claude", func(consent permission) (conversation, error) {
		talk.consent, talk.allowed = consent, consent
		return talk, nil
	})
	opened, _ := press(t, m, "enter")
	if opened.page == nil {
		t.Fatal("enter on a reading did not open its page")
	}
	if !strings.Contains(plain(opened.content()), "ask about this") {
		t.Fatalf("the page does not offer the conversation:\n%s", plain(opened.content()))
	}

	asked, _ := press(t, opened, "a")
	if asked.view != viewAsk {
		t.Fatalf("view = %q, want ask", asked.view)
	}
	if asked.page != nil {
		t.Fatal("the page was left open behind the conversation")
	}
	question := asked.talk.prompt.Value()
	if !strings.Contains(question, "About") || !strings.Contains(question, "worth doing about it") {
		t.Fatalf("prompt = %q, want the reading turned into a question", question)
	}
	if len(talk.asked) != 0 {
		t.Fatal("the question was sent before anybody read it")
	}
}

func TestAPageOffersNothingWithoutAnAssistant(t *testing.T) {
	m := loaded(t, healthy())
	opened, _ := press(t, m, "enter")
	if opened.page == nil {
		t.Fatal("enter on a reading did not open its page")
	}
	if strings.Contains(plain(opened.content()), "ask about this") {
		t.Fatal("a key that would do nothing was drawn")
	}
	unchanged, _ := press(t, opened, "a")
	if unchanged.view == viewAsk {
		t.Fatal("a page with no assistant behind it opened the conversation anyway")
	}
}

func TestASubjectCarriesTheStatement(t *testing.T) {
	page := details{title: "the statement", code: "SELECT * FROM orders"}
	subject := page.subject()
	if !strings.Contains(subject, "for the statement below") {
		t.Fatalf("subject = %q, want it to say the statement follows", subject)
	}
	if !strings.HasSuffix(subject, "SELECT * FROM orders") {
		t.Fatalf("subject = %q, want the statement at the end", subject)
	}
}

func TestAskReportsAnAssistantItCannotOpen(t *testing.T) {
	broken := errors.New("the local model is not downloaded")
	m := loaded(t, healthy()).WithAssistant("here", func(permission) (conversation, error) {
		return nil, broken
	})
	opened, _ := m.show(viewAsk)
	typed := typeInto(t, opened.(Model), "hello")
	sent, cmd := press(t, typed, "enter")

	if cmd != nil {
		t.Fatal("a turn was started against an assistant that could not be opened")
	}
	if !strings.Contains(said(t, sent), "not downloaded") {
		t.Fatalf("body = %q, want it to say why there is nothing to talk to", said(t, sent))
	}
}

func TestTheAssistantIsNotOpenedUntilItIsNeeded(t *testing.T) {
	opened := 0
	m := loaded(t, healthy()).WithAssistant("here", func(permission) (conversation, error) {
		opened++
		return &scripted{events: []agent.Event{{Kind: agent.EventDone, Stop: ai.StopEndTurn}}}, nil
	})
	if opened != 0 {
		t.Fatal("a model was loaded before anybody asked anything")
	}
	shown, _ := m.show(viewAsk)
	typed := typeInto(t, shown.(Model), "hello")
	asked, cmd := press(t, typed, "enter")
	converse(t, asked, cmd)
	if opened != 1 {
		t.Fatalf("the assistant was opened %d times, want once and only when needed", opened)
	}
}

func TestGateStopsWhenTheTurnIs(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	asking := gate{asks: make(chan approval)}
	if err := asking.Allow(ctx, agent.Outbound{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("Allow() error = %v, want context.Canceled while nobody is listening", err)
	}

	waiting, stop := context.WithCancel(context.Background())
	asks := make(chan approval, 1)
	go func() {
		<-asks
		stop()
	}()
	if err := (gate{asks: asks}).Allow(waiting, agent.Outbound{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("Allow() error = %v, want context.Canceled while waiting for an answer", err)
	}
}

func TestConsentBodyNamesWhatWouldGo(t *testing.T) {
	written := consentBody(agent.Outbound{
		Instance: "claude",
		Model:    "claude-sonnet-5",
		Classes:  []agent.Class{agent.ClassRows, agent.ClassQuestion},
		Bytes:    4096,
	})
	for _, want := range []string{"claude-sonnet-5", "somebody else's machine", string(agent.ClassRows), "4 KiB"} {
		if !strings.Contains(written, want) {
			t.Fatalf("the question does not hold %q:\n%s", want, written)
		}
	}
	if !strings.Contains(consentBody(agent.Outbound{Bytes: 12}), "12 bytes") {
		t.Fatal("a small payload was not measured in bytes")
	}
}

func TestARecordWithNothingToRecordOn(t *testing.T) {
	var empty chat
	empty.record(agent.Event{Kind: agent.EventText, Text: "nowhere to put this"})
	empty.ended(nil)
	if empty.last() != nil {
		t.Fatal("an exchange appeared out of nothing")
	}
}

func TestAnAnswerThatCameBackEmpty(t *testing.T) {
	talk := &scripted{events: []agent.Event{{Kind: agent.EventDone, Stop: ai.StopEndTurn}}}
	m := talking(t, talk)
	m = typeInto(t, m, "say nothing")
	m, cmd := press(t, m, "enter")
	m = converse(t, m, cmd)
	if !strings.Contains(said(t, m), "nothing came back") {
		t.Fatalf("body = %q, want it to say the model said nothing", said(t, m))
	}
}

func TestStepWritesOutACall(t *testing.T) {
	if got := step(ai.ToolCall{Name: "list_schemas"}); got != "list_schemas" {
		t.Fatalf("step() = %q, want just the name when there is nothing to pass", got)
	}
	got := step(ai.ToolCall{Name: "run_select", Arguments: map[string]any{"limit": 10, "statement": "SELECT 1"}})
	if !strings.HasPrefix(got, "run_select(limit: 10, statement: SELECT 1") {
		t.Fatalf("step() = %q, want the arguments in a settled order", got)
	}
}

func TestAskScrollsWithoutSendingAnything(t *testing.T) {
	talk := &scripted{}
	m := talking(t, talk)
	moved, _ := press(t, m, "pgdown")
	if len(talk.asked) != 0 {
		t.Fatal("scrolling asked something")
	}
	if moved.view != viewAsk {
		t.Fatal("scrolling left the screen")
	}
}

func TestAskOpensThePalette(t *testing.T) {
	m := talking(t, &scripted{})
	opened, _ := press(t, m, "ctrl+k")
	if opened.palette == nil {
		t.Fatal("the palette did not open from the conversation")
	}
}

func TestAskScrollsALongConversation(t *testing.T) {
	events := make([]agent.Event, 0, 60)
	for range 60 {
		events = append(events, agent.Event{Kind: agent.EventText, Text: "a line of an answer\n"})
	}
	events = append(events, agent.Event{Kind: agent.EventDone, Stop: ai.StopEndTurn})
	m := talking(t, &scripted{events: events})
	m = typeInto(t, m, "say a lot")
	m, cmd := press(t, m, "enter")
	m = converse(t, m, cmd)

	back, _ := press(t, m, "pgup")
	if !strings.Contains(said(t, back), "more") {
		t.Fatalf("a conversation scrolled back does not say there is more below:\n%s", said(t, back))
	}
	if back.talk.prompt.Value() != "" {
		t.Fatal("scrolling typed into the box")
	}
	forward, _ := press(t, back, "pgdown")
	if forward.offset <= back.offset {
		t.Fatal("the conversation did not scroll back down")
	}
}

func TestAskAboutWithNothingToAskYet(t *testing.T) {
	m := loaded(t, healthy())
	asked, _ := m.askAbout("about a reading")
	if asked.(Model).talk.prompt.Value() != "" {
		t.Fatal("a question was written into a box with nothing behind it")
	}
	if asked.(Model).view != viewAsk {
		t.Fatal("the screen did not open")
	}
}

func TestConsentIsAnsweredOnce(t *testing.T) {
	talk := &scripted{gated: true, class: agent.ClassQuestion}
	m := talking(t, talk)
	m = typeInto(t, m, "which?")
	m, cmd := press(t, m, "enter")
	m = converse4Approval(t, m, cmd)

	waiting := m.talk.pending
	if waiting == nil {
		t.Fatal("nothing is waiting")
	}
	ignored, _ := m.Update(askAnswerMsg{answer: waiting.answer, token: m.talk.token - 1, on: m.id})
	if ignored.(Model).talk.pending != nil {
		t.Fatal("an answer from an abandoned turn cleared the question")
	}
}

// TestCtrlCAsksBeforeItLeaves is the same promise the editor makes: a key that
// closes the program in every other terminal program closes this one too, and
// it asks first, because a conversation and an unsent question are worth one
// keypress of confirmation.
func TestCtrlCAsksBeforeItLeaves(t *testing.T) {
	m := talking(t, &scripted{})
	m.width, m.height = 100, 32
	asked, _ := press(t, m, "ctrl+c")
	if asked.modal == nil {
		t.Fatal("ctrl+c left without asking")
	}
	if asked.quitting {
		t.Fatal("ctrl+c quit outright")
	}
	if !strings.Contains(plain(asked.modal.view(m.width)), "close opendba?") {
		t.Fatalf("the question is not the one the rest of the program asks:\n%s",
			plain(asked.modal.view(m.width)))
	}
}

// TestTheLibraryIsUnpackedOnTheWayIn is the start-up bargain: nothing is
// written for somebody who never asks a question, and nobody who does ask waits
// for it, because it happens while the screen is being read.
func TestTheLibraryIsUnpackedOnTheWayIn(t *testing.T) {
	if !local.Carried() {
		t.Skip("this program carries no inference library for this machine")
	}
	m := configured(t)
	if m.session.AI.Library.Present() {
		t.Fatal("the library was already on disk before the conversation was opened")
	}
	m.ai.warmed = false
	opened, _ := m.show(viewAsk)
	warm := opened.(Model)
	if !warm.ai.warmed {
		t.Fatal("opening the conversation did not ask for the library")
	}
	_, unpack := m.warming()
	if unpack == nil {
		t.Fatal("nothing was going to unpack it")
	}
	msg, ok := unpack().(warmedMsg)
	if !ok {
		t.Fatal("the library was not asked for")
	}
	if msg.err != nil {
		t.Fatalf("unpacking the library failed: %v", msg.err)
	}
	if !warm.session.AI.Library.Present() {
		t.Fatalf("the library is not there, missing %v", warm.session.AI.Library.Missing())
	}
	done, _ := warm.warmed(msg)
	if got := done.(Model).library4AI(); got != "library "+local.Build {
		t.Fatalf("library4AI() = %q, want it to say the library is here", got)
	}
	again, _ := done.(Model).warming()
	if _, cmd := again.warming(); cmd != nil {
		t.Fatal("the library is unpacked again every time the conversation is opened")
	}
}

func TestATroubledUnpackingIsWrittenDownRatherThanRaised(t *testing.T) {
	m := configured(t)
	done, _ := m.warmed(warmedMsg{err: errors.New("the disk is full")})
	if got := done.(Model).library4AI(); !strings.Contains(got, "no build") {
		t.Fatalf("library4AI() = %q, want the group to say something is wrong", got)
	}
	if done.(Model).ai.library.trouble != "the disk is full" {
		t.Fatalf("trouble = %q", done.(Model).ai.library.trouble)
	}
}

// local4Talking is the conversation screen pointed at a model that runs here,
// with the load held open so a test can look at the screen while it happens.
func local4Talking(t *testing.T, talk *scripted) (Model, tea.Cmd) {
	t.Helper()
	m := configured(t)
	m.width, m.height = 100, 32
	m.view = viewAsk
	m.talk.instance = "here"
	m.build = func(consent permission) (conversation, error) {
		talk.consent = consent
		return talk, nil
	}
	loading, cmd := m.load4Talk()
	if cmd == nil {
		t.Fatal("choosing a model that runs here loaded nothing")
	}
	return loading, cmd
}

// TestTheBoxIsShutWhileAModelLoads is what the state is for: a question typed
// into a box while gigabytes are being read would sit there until the read
// finished, and a box that takes words and does nothing with them reads as a
// program that has stopped.
func TestTheBoxIsShutWhileAModelLoads(t *testing.T) {
	held := make(chan struct{})
	loading, cmd := local4Talking(t, &scripted{warming: held})
	if !loading.talk.loading {
		t.Fatal("the screen does not know a model is being read")
	}
	if loading.talk.prompt.Focused() {
		t.Fatal("the box still has the keys")
	}
	body := plain(loading.askBody())
	for _, want := range []string{"loading", "stop loading"} {
		if !strings.Contains(body, want) {
			t.Fatalf("body does not say %q:\n%s", want, body)
		}
	}
	typed, _ := press(t, loading, "a")
	if typed.talk.prompt.Value() != "" {
		t.Fatalf("the box took %q while the model was being read", typed.talk.prompt.Value())
	}
	if loading.typed(80) == loading.talk.prompt.View() {
		t.Fatal("the box looks ready while the keys do nothing")
	}
	if !strings.Contains(plain(loading.typed(80)), "ask about this database") {
		t.Fatalf("the box lost what it says: %q", plain(loading.typed(80)))
	}
	if strings.Contains(loading.boxed("x", 20), plain4Accent(loading)) {
		t.Fatal("the border is still lit while nothing can be typed")
	}
	close(held)
	if _, ok := runFirst(t, cmd).(readyMsg); !ok {
		t.Fatal("nothing was going to say the model had arrived")
	}
}

// plain4Accent is the accent colour as it appears in a rendered line, which is
// how a test tells a lit border from an unlit one.
func plain4Accent(m Model) string { return sgr(m.theme.Accent.Render("|")) }

// sgr is the colour out of a rendered run, without the letter that ends the
// escape: a run that carries a background as well ends differently and is still
// the same colour.
func sgr(rendered string) string {
	after, ok := strings.CutPrefix(rendered, "\x1b[")
	if !ok {
		return rendered
	}
	numbers, _, _ := strings.Cut(after, "m")
	return numbers
}

func TestAModelThatHasLoadedHandsTheBoxBack(t *testing.T) {
	talk := &scripted{}
	loading, cmd := local4Talking(t, talk)
	done, _ := loading.Update(runFirst(t, cmd))
	ready := done.(Model)
	if ready.talk.loading {
		t.Fatal("the screen still thinks it is loading")
	}
	if !ready.talk.loaded {
		t.Fatal("the screen does not know it is holding a model")
	}
	if !talk.warmed {
		t.Fatal("the model was not read before the first question")
	}
	if !ready.talk.prompt.Focused() {
		t.Fatal("the box did not get the keys back")
	}
	if !ready.talk.loaded {
		t.Fatal("the screen does not know it is holding a model")
	}
}

// TestReleasingGivesTheMemoryBack is the other half: a model that runs here
// holds gigabytes for as long as it is loaded, and finishing with it should not
// mean leaving the program.
func TestReleasingGivesTheMemoryBack(t *testing.T) {
	talk := &scripted{}
	loading, cmd := local4Talking(t, talk)
	done, _ := loading.Update(runFirst(t, cmd))
	released, notice := done.(Model).released()
	if !talk.closed {
		t.Fatal("the model was dropped without being let go of")
	}
	if released.assistant != nil || released.talk.loaded {
		t.Fatal("the screen still thinks it holds a model")
	}
	if notice == nil {
		t.Fatal("nothing said what happened")
	}
	if strings.Contains(plain(released.askBody()), "release") {
		t.Fatalf("it still offers to release what it let go of:\n%s", plain(released.askBody()))
	}
}

func TestALoadCanBeGivenUpOn(t *testing.T) {
	held := make(chan struct{})
	defer close(held)
	loading, cmd := local4Talking(t, &scripted{warming: held})
	stopped, _ := press(t, loading, "esc")
	msg, ok := runFirst(t, cmd).(readyMsg)
	if !ok {
		t.Fatal("the load said nothing when it was stopped")
	}
	if msg.err == nil {
		t.Fatal("the load carried on after it was stopped")
	}
	done, _ := stopped.Update(msg)
	after := done.(Model)
	if after.talk.loading || after.talk.loaded {
		t.Fatal("the screen still thinks something is happening")
	}
	if after.talk.trouble != "" {
		t.Fatalf("trouble = %q, want giving up to be quiet", after.talk.trouble)
	}
	if !after.talk.prompt.Focused() {
		t.Fatal("the box did not get the keys back")
	}
}

func TestALoadThatFailedSaysSo(t *testing.T) {
	loading, cmd := local4Talking(t, &scripted{warmErr: errors.New("the file is not a model")})
	done, _ := loading.Update(runFirst(t, cmd))
	after := done.(Model)
	if !strings.Contains(after.talk.trouble, "not a model") {
		t.Fatalf("trouble = %q", after.talk.trouble)
	}
	if after.talk.loaded {
		t.Fatal("a model that would not load is being held")
	}
	if !strings.Contains(plain(after.askBody()), "not a model") {
		t.Fatalf("the failure is nowhere on the screen:\n%s", plain(after.askBody()))
	}
}

// TestSomethingSomewhereElseIsNotLoaded keeps the state honest: opening a
// client that talks to a server is building a struct, and calling that loading
// would put a spinner on the screen for no reason at all.
func TestSomethingSomewhereElseIsNotLoaded(t *testing.T) {
	m := configured(t)
	m.talk.instance = "claude"
	m.build = func(permission) (conversation, error) { return &scripted{}, nil }
	loading, cmd := m.load4Talk()
	if cmd != nil || loading.talk.loading {
		t.Fatal("a back-end on somebody else's machine was read off this one")
	}
}

// TestTheWorkingIsFoldedAway is the shape of a reasoning model on a screen with
// a conversation on it: three paragraphs of deliberation above two lines of
// reply would bury the reply, so the working is a line until it is asked for.
func TestTheWorkingIsFoldedAway(t *testing.T) {
	talk := &scripted{events: []agent.Event{
		{Kind: agent.EventReasoning, Text: "the orders table is the one with the dates in it\nso I should look there first"},
		{Kind: agent.EventText, Text: "Look at orders."},
		{Kind: agent.EventDone},
	}}
	m := talking(t, talk)
	m.width, m.height = 100, 32
	m = typeInto(t, m, "where are the dates?")
	asked, cmd := press(t, m, "enter")
	done := converse(t, asked, cmd)

	folded := plain(done.askBody())
	if !strings.Contains(folded, "▸ thinking") {
		t.Fatalf("the working is not offered:\n%s", folded)
	}
	if strings.Contains(folded, "look there first") {
		t.Fatalf("the working is in the way of the answer:\n%s", folded)
	}
	opened, _ := press(t, done, "ctrl+t")
	shown := plain(opened.askBody())
	if !strings.Contains(shown, "look there first") {
		t.Fatalf("ctrl+t did not open the working:\n%s", shown)
	}
	if !strings.Contains(shown, "Look at orders.") {
		t.Fatalf("the answer went with it:\n%s", shown)
	}
	closed, _ := press(t, opened, "ctrl+t")
	if strings.Contains(plain(closed.askBody()), "look there first") {
		t.Fatal("ctrl+t is one way only")
	}
}

// TestTheWorkingIsShownWhileItIsTheOnlyThingThereIs is the other half: a model
// that has been reasoning for thirty seconds and shown nothing is
// indistinguishable from one that has hung.
func TestTheWorkingIsShownWhileItIsTheOnlyThingThereIs(t *testing.T) {
	m := talking(t, &scripted{})
	m.width, m.height = 100, 32
	m.talk.busy = true
	m.talk.exchanges = []exchange{{
		question:  "where are the dates?",
		reasoning: "first line\nsecond line\nthird line\nfourth line\nthe latest thought",
	}}
	body := plain(m.askBody())
	if !strings.Contains(body, "the latest thought") {
		t.Fatalf("nothing shows the model is working:\n%s", body)
	}
	if strings.Contains(body, "first line") {
		t.Fatalf("the whole of it is on screen rather than where it has got to:\n%s", body)
	}
}

// TestTheModelIsNamedTheWayTheCatalogueNamesIt is a small thing that says the
// program knows what it is running: gemma-4-e4b-qat is a directory on a disk.
func TestTheModelIsNamedTheWayTheCatalogueNamesIt(t *testing.T) {
	m := configured(t)
	m.talk.instance = "here"
	if got := m.model4Meta(); got != "Gemma 4 E4B" {
		t.Fatalf("model4Meta() = %q, want the name it has everywhere else", got)
	}
	m.talk.instance = "claude"
	if got := m.model4Meta(); got != "claude-sonnet-5" {
		t.Fatalf("model4Meta() = %q, want what a hosted instance answers with", got)
	}
	m.talk.instance = "nobody"
	if got := m.model4Meta(); got != "" {
		t.Fatalf("model4Meta() = %q", got)
	}
}

func TestAModelThatWillNotLetGoSaysSo(t *testing.T) {
	talk := &scripted{}
	loading, cmd := local4Talking(t, talk)
	done, _ := loading.Update(runFirst(t, cmd))
	held := done.(Model)
	held.assistant = &stubborn{}
	released, _ := held.released()
	if released.talk.trouble == "" {
		t.Fatal("a model that would not let go was quietly forgotten")
	}
	if released.assistant != nil {
		t.Fatal("it is still being held")
	}
}

// stubborn is a conversation whose back-end will not release what it holds.
type stubborn struct{ scripted }

func (s *stubborn) Close() error { return errors.New("the memory is still mapped") }

// TestTheBoxIsShutWhileAnAnswerArrives is the same closing as a model being
// read in, for the same reason: what it is doing is said once, on the line
// under the box, and the box itself is plainly not for typing into yet.
func TestTheBoxIsShutWhileAnAnswerArrives(t *testing.T) {
	held := make(chan struct{})
	defer close(held)
	talk := &scripted{held: held, events: []agent.Event{{Kind: agent.EventText, Text: "looking"}}}
	m := talking(t, talk)
	m.width, m.height = 100, 32
	m = typeInto(t, m, "which is biggest")
	busy, cmd := press(t, m, "enter")
	if !busy.talk.busy {
		t.Fatal("the screen does not think it is answering")
	}
	if cmd == nil {
		t.Fatal("nothing was started")
	}

	body := plain(busy.askBody())
	if !strings.Contains(body, "esc") || !strings.Contains(body, "cancel") {
		t.Fatalf("nothing offers to stop it:\n%s", body)
	}
	if strings.Count(body, "thinking") > 1 {
		t.Fatalf("it says what it is doing twice:\n%s", body)
	}
	if busy.typed(80) == busy.talk.prompt.View() {
		t.Fatal("the box looks ready to type into while an answer is arriving")
	}
	typed, _ := press(t, busy, "x")
	if typed.talk.prompt.Value() != "" {
		t.Fatalf("the box took %q while an answer was arriving", typed.talk.prompt.Value())
	}
	stopped, _ := press(t, busy, "esc")
	done := converse(t, stopped, cmd)
	if done.talk.busy {
		t.Fatal("esc did not stop the answer")
	}
	if !done.talk.exchanges[0].cancelled {
		t.Fatalf("the turn is not marked as stopped: %+v", done.talk.exchanges[0])
	}
	if !done.talk.prompt.Focused() {
		t.Fatal("the box did not get the keys back")
	}
}

// TestScrollingAndThinkingStillWorkWhileBusy keeps the closing from going too
// far: reading what has arrived so far is not typing.
func TestScrollingAndThinkingStillWorkWhileBusy(t *testing.T) {
	m := talking(t, &scripted{})
	m.width, m.height = 100, 32
	m.talk.busy = true
	m.talk.exchanges = []exchange{{question: "q", reasoning: "a thought", answer: "an answer"}}
	opened, _ := press(t, m, "ctrl+t")
	if !opened.talk.thinking {
		t.Fatal("the working cannot be opened while the model is still working")
	}
	m.offset = 5
	if scrolled, _ := press(t, m, "pgup"); scrolled.offset == 5 {
		t.Fatal("the conversation cannot be walked back while an answer arrives")
	}
}

// TestTheConversationCanBeWalkedBack is what pgup is for, and what following
// the end of an answer must not take away: an answer arriving while somebody is
// reading further up must leave them where they are.
func TestTheConversationCanBeWalkedBack(t *testing.T) {
	m := talking(t, &scripted{})
	m.width, m.height = 100, 24
	for i := range 12 {
		m.talk.exchanges = append(m.talk.exchanges, exchange{
			question: "question " + string(rune('a'+i)),
			answer:   strings.Repeat("a line of an answer\n", 4),
			done:     true,
		})
	}
	m = m.pinned()
	if m.offset == 0 {
		t.Fatal("a conversation twelve exchanges long fits on a screen of twenty four rows")
	}
	end := m.offset

	up, _ := press(t, m, "pgup")
	if up.offset >= end {
		t.Fatalf("offset = %d, want it walked back from %d", up.offset, end)
	}
	if plain(up.askBody()) == plain(m.askBody()) {
		t.Fatal("the screen did not move")
	}

	stayed := up.pinned()
	if stayed.offset != up.offset {
		t.Fatalf("offset = %d, want it left at %d while an answer arrived", stayed.offset, up.offset)
	}

	down, _ := press(t, up, "pgdown")
	if down.offset <= up.offset {
		t.Fatal("pgdown did not walk forward")
	}
	if followed := down.pinned(); followed.offset != down.offset {
		t.Fatal("coming back to the end did not start following it again")
	}
}

// TestWhoSaidWhatIsDrawnRatherThanLabelled is what tells the two sides of a
// conversation apart at a glance.
func TestWhoSaidWhatIsDrawnRatherThanLabelled(t *testing.T) {
	m := talking(t, &scripted{})
	m.width, m.height = 100, 32
	m.talk.exchanges = []exchange{{question: "ile mam tabel?", answer: "four", done: true}}
	drawn := m.exchangeView(m.talk.exchanges[0], 80, true)
	asked := strings.Split(drawn, "\n")[0]
	if !strings.Contains(plain(asked), "▌ ile mam tabel?") {
		t.Fatalf("the question has no bar down its side: %q", plain(asked))
	}
	if !strings.Contains(asked, plain4Accent(m)) {
		t.Fatalf("the bar is not the accent colour: %q", asked)
	}
	if !strings.Contains(asked, ground4Test(m.theme.P.Surface)) {
		t.Fatalf("the question has no ground behind it: %q", asked)
	}
	if lipgloss.Width(asked) != 80 {
		t.Fatalf("the question is %d wide in a screen of 80", lipgloss.Width(asked))
	}
	if strings.Contains(plain(drawn), "you") {
		t.Fatalf("the question is labelled as well as drawn:\n%s", plain(drawn))
	}
}

// ground4Test is a background as it appears in a rendered line.
func ground4Test(ground color.Color) string {
	return sgr(lipgloss.NewStyle().Background(ground).Render("|"))
}

// TestAStatementIsShownBeforeItRuns is what makes a model that writes its own
// SQL a reasonable thing to have: whatever it thought of, somebody reads it
// before the database does.
func TestAStatementIsShownBeforeItRuns(t *testing.T) {
	talk := &scripted{
		statement: "SELECT count(*) FROM orders",
		events:    []agent.Event{{Kind: agent.EventText, Text: "four"}, {Kind: agent.EventDone}},
	}
	m := talking(t, talk)
	m.width, m.height = 100, 32
	m = typeInto(t, m, "how many orders?")
	asked, cmd := press(t, m, "enter")
	waiting := converse4Approval(t, asked, cmd)

	body := plain(waiting.askBody())
	for _, want := range []string{"run this?", "SELECT count(*) FROM orders", "run it", "do not run it"} {
		if !strings.Contains(body, want) {
			t.Fatalf("the question does not say %q:\n%s", want, body)
		}
	}
	if talk.ran {
		t.Fatal("the statement ran before anybody was asked")
	}
	allowed, cmd := press(t, waiting, "enter")
	allowed = converse(t, allowed, cmd)
	if !talk.ran {
		t.Fatal("saying yes did not let the statement run")
	}
	if !strings.Contains(said(t, allowed), "four") {
		t.Fatalf("body = %q, want the answer once the statement had run", said(t, allowed))
	}
}

func TestAStatementCanBeRefused(t *testing.T) {
	talk := &scripted{statement: "SELECT * FROM billing"}
	m := talking(t, talk)
	m.width, m.height = 100, 32
	m = typeInto(t, m, "show me billing")
	asked, cmd := press(t, m, "enter")
	waiting := converse4Approval(t, asked, cmd)

	refused, cmd := press(t, waiting, "esc")
	refused = converse(t, refused, cmd)
	if talk.ran {
		t.Fatal("the statement ran after it was refused")
	}
	if talk.refused == nil {
		t.Fatal("the assistant was not told the statement was refused")
	}
	if refused.talk.pending != nil {
		t.Fatal("the question is still up")
	}
	if !refused.talk.prompt.Focused() {
		t.Fatal("the box did not get the keys back")
	}
}

// TestTheBoxIsAWholeBlock is what the box is made of: a bar down the side and a
// ground behind every cell of it, including the rows the text field pads out
// for itself, which it pads with nothing at all.
func TestTheBoxIsAWholeBlock(t *testing.T) {
	m := talking(t, &scripted{})
	m.width, m.height = 100, 32
	m.talk.prompt.SetWidth(ui.TextWidth(m.width) - 4)
	block := strings.Split(m.foot(ui.FrameWidth(m.width)), "\n")
	if len(block) != boxRows {
		t.Fatalf("the box is %d rows, want %d", len(block), boxRows)
	}
	for at, line := range block {
		if lipgloss.Width(line) != ui.FrameWidth(m.width) {
			t.Fatalf("row %d is %d wide, want %d", at, lipgloss.Width(line), ui.FrameWidth(m.width))
		}
		if !strings.HasPrefix(plain(line), "┃") {
			t.Fatalf("row %d has no bar down its side: %q", at, plain(line))
		}
		if strings.Count(line, ground4Test(m.theme.P.Surface)) == 0 {
			t.Fatalf("row %d has nothing behind it: %q", at, line)
		}
	}
	if !strings.Contains(block[1], plain4Accent(m)) {
		t.Fatalf("the bar is not lit while the box has the keys: %q", block[1])
	}
	m.talk.busy = true
	if strings.Contains(strings.Split(m.foot(ui.FrameWidth(m.width)), "\n")[1], plain4Accent(m)) {
		t.Fatal("the bar is still lit while an answer is arriving")
	}
}

// TestTheWheelWalksTheConversation is the mouse doing what pgup does. A
// terminal program that ignores the wheel is one people scroll with nothing
// happening, which reads as a program that has stopped.
func TestTheWheelWalksTheConversation(t *testing.T) {
	m := talking(t, &scripted{})
	m.width, m.height = 100, 24
	for i := range 12 {
		m.talk.exchanges = append(m.talk.exchanges, exchange{
			question: "question " + string(rune('a'+i)),
			answer:   strings.Repeat("a line of an answer\n", 4),
			done:     true,
		})
	}
	m = m.pinned()
	end := m.offset

	up, _ := m.wheeled(tea.MouseWheelMsg{Button: tea.MouseWheelUp})
	rolled := up.(Model)
	if rolled.offset != end-wheelRows {
		t.Fatalf("offset = %d, want %d rows back from %d", rolled.offset, wheelRows, end)
	}
	down, _ := rolled.wheeled(tea.MouseWheelMsg{Button: tea.MouseWheelDown})
	if down.(Model).offset != end {
		t.Fatalf("offset = %d, want it back at %d", down.(Model).offset, end)
	}
	sideways, _ := rolled.wheeled(tea.MouseWheelMsg{Button: tea.MouseWheelLeft})
	if sideways.(Model).offset != rolled.offset {
		t.Fatal("a sideways wheel moved the conversation")
	}
}

func TestTheWheelWalksEveryOtherScreenToo(t *testing.T) {
	m := loaded(t, healthy())
	m.width, m.height = 100, 20
	opened, _ := m.show(viewHelp)
	help := opened.(Model)
	down, _ := help.wheeled(tea.MouseWheelMsg{Button: tea.MouseWheelDown})
	if down.(Model).offset != wheelRows {
		t.Fatalf("offset = %d, want the help walked down", down.(Model).offset)
	}
}

// TestTheAnswerDoesNotTouchTheBox is a gap of one row, which is the difference
// between a conversation and a wall of text with a box wedged into it.
func TestTheAnswerDoesNotTouchTheBox(t *testing.T) {
	m := talking(t, &scripted{})
	m.width, m.height = 100, 24
	m.talk.exchanges = []exchange{{
		question: "q",
		answer:   strings.Repeat("a line of an answer\n", 40),
		done:     true,
	}}
	m = m.pinned()
	lines := strings.Split(plain(m.askBody()), "\n")
	box := -1
	for at, line := range lines {
		if strings.HasPrefix(line, "┃") {
			box = at
			break
		}
	}
	if box < 1 {
		t.Fatalf("the box is not there:\n%s", plain(m.askBody()))
	}
	if strings.TrimSpace(lines[box-1]) != "" {
		t.Fatalf("the answer runs into the box: %q", lines[box-1])
	}
}

// TestTheModelIsReadInOnTheFirstQuestion is the bargain: opening the
// conversation to read what was said before costs nothing, and the wait for a
// model happens when there is something to ask it.
func TestTheModelIsReadInOnTheFirstQuestion(t *testing.T) {
	talk := &scripted{events: []agent.Event{{Kind: agent.EventText, Text: "four"}, {Kind: agent.EventDone}}}
	m := configured(t)
	m.width, m.height = 100, 32
	m.talk.instance = "here"
	m.build = func(allowed permission) (conversation, error) {
		talk.consent, talk.allowed = allowed, allowed
		return talk, nil
	}

	opened, _ := m.show(viewAsk)
	waiting := opened.(Model)
	if waiting.talk.loading || waiting.assistant != nil {
		t.Fatal("opening the conversation read a model into memory")
	}

	waiting = typeInto(t, waiting, "how many tables?")
	asking, cmd := press(t, waiting, "enter")
	if !asking.talk.loading {
		t.Fatal("asking did not start reading the model in")
	}
	if asking.talk.waiting != "how many tables?" {
		t.Fatalf("waiting = %q, want the question kept", asking.talk.waiting)
	}
	if len(asking.talk.exchanges) != 0 {
		t.Fatal("the turn started before there was anything to ask")
	}
	if !strings.Contains(plain(asking.askBody()), "how many tables?") {
		t.Fatalf("the question is not on screen while the model is read in:\n%s", plain(asking.askBody()))
	}

	done := converse(t, asking, cmd)
	if done.talk.loading || done.talk.waiting != "" {
		t.Fatalf("the screen is still waiting: %+v", done.talk.waiting)
	}
	if len(talk.asked) != 1 || talk.asked[0] != "how many tables?" {
		t.Fatalf("asked = %v, want the question that was waiting", talk.asked)
	}
	if !strings.Contains(said(t, done), "four") {
		t.Fatalf("body = %q, want the answer", said(t, done))
	}
	if done.talk.prompt.Value() != "" {
		t.Fatalf("the box still holds %q", done.talk.prompt.Value())
	}
}

// TestAQuestionSurvivesAModelThatWillNotLoad keeps somebody's words: a model
// that would not open is a reason to try something else, not to lose what was
// typed.
func TestAQuestionSurvivesAModelThatWillNotLoad(t *testing.T) {
	m := configured(t)
	m.width, m.height = 100, 32
	m.talk.instance = "here"
	m.build = func(permission) (conversation, error) {
		return nil, errors.New("the file is not a model")
	}
	m = typeInto(t, m.show4Test(viewAsk), "how many tables?")
	asking, cmd := press(t, m, "enter")
	done := converse(t, asking, cmd)
	if !strings.Contains(done.talk.trouble, "not a model") {
		t.Fatalf("trouble = %q", done.talk.trouble)
	}
	if done.talk.prompt.Value() != "how many tables?" {
		t.Fatalf("the box holds %q, want the question still there", done.talk.prompt.Value())
	}
}

// show4Test opens a screen and throws away the command, which a test that is
// about something else does not need.
func (m Model) show4Test(target view) Model {
	opened, _ := m.show(target)
	return opened.(Model)
}
