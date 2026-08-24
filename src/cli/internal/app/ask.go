package app

import (
	"context"
	"errors"
	"fmt"
	"runtime/debug"
	"sort"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/sonquer/tui4db/src/cli/internal/ai"
	"github.com/sonquer/tui4db/src/cli/internal/ai/agent"
	"github.com/sonquer/tui4db/src/cli/internal/ai/providers/local"
	"github.com/sonquer/tui4db/src/cli/internal/chats"
	"github.com/sonquer/tui4db/src/cli/internal/ui"
)

const (
	promptRows = 3

	// stepWidth is how much of a tool call is shown on the line that says one was
	// made.
	stepWidth = 64
)

// errRefused is what the assistant is told when a turn is not allowed to leave
// the machine.
var errRefused = errors.New("you did not allow this to be sent")

// conversation is what the Ask screen talks to.
type conversation interface {
	Ask(ctx context.Context, question string, out chan<- agent.Event) error

	// Warm makes the back-end ready before a question rather than during one.
	Warm(ctx context.Context) error

	// Close lets go of what the back-end is holding, which for a model running
	// here is the memory it was loaded into.
	Close() error
}

// remembering is a conversation that can hand back what has been said and take
// it back again, which is what keeping one and opening it later needs.
type remembering interface {
	Messages() []ai.Message
	Resume(messages []ai.Message)
}

// recalls asks a conversation whether it is one that remembers.
func recalls(talk conversation) (remembering, bool) {
	held, ok := talk.(remembering)
	return held, ok
}

// approval is a question the assistant asks the screen and waits for.
type approval struct {
	outbound  *agent.Outbound
	statement string
	answer    chan error
}

// permission is what the assistant asks before it does either of the two things
// it is not allowed to do on its own.
type permission interface {
	agent.Consent
	Statement(ctx context.Context, statement string) error
}

// gate is what the assistant asks before a turn leaves the machine or a
// statement reaches the database.
type gate struct{ asks chan approval }

// Allow puts the question to the screen and waits for the person to answer it.
func (g gate) Allow(ctx context.Context, outbound agent.Outbound) error {
	return g.ask(ctx, approval{outbound: &outbound})
}

// Statement asks whether a statement the assistant wrote may run.
func (g gate) Statement(ctx context.Context, statement string) error {
	return g.ask(ctx, approval{statement: statement})
}

func (g gate) ask(ctx context.Context, question approval) error {
	question.answer = make(chan error, 1)
	select {
	case g.asks <- question:
	case <-ctx.Done():
		return ctx.Err()
	}
	select {
	case err := <-question.answer:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

// stream is one running turn: what it produces, what it wants permission for,
// and how it ended.
type stream struct {
	events    <-chan agent.Event
	approvals <-chan approval
	failed    <-chan error
}

type askEventMsg struct {
	event agent.Event
	token int
}

type askApprovalMsg struct {
	request approval
	token   int
}

type askEndedMsg struct {
	err   error
	token int
}

type askAnswerMsg struct {
	answer chan error
	err    error
	token  int
}

// exchange is one question and what came back.
type exchange struct {
	question  string
	answer    string
	reasoning string
	steps     []string
	failed    string
	done      bool
	cancelled bool
}

// chat is the conversation screen.
type chat struct {
	theme     *ui.Theme
	prompt    textarea.Model
	exchanges []exchange
	busy      bool
	token     int
	instance  string
	trouble   string
	running   stream

	// loading is a model on its way into memory.
	loading bool

	// loaded is a back-end that is ready, which is what makes letting go of it
	// something there is any point offering.
	loaded bool

	// waiting is a question asked while the model it is for is still being read
	// into memory.
	waiting string

	// bottom is where the end of the conversation was the last time it was looked
	// at, which is how following it is told apart from having been left there.
	bottom int

	// thinking is whether the reasoning a model shows its working in is opened out.
	thinking bool

	// asks is how the assistant reaches the person for permission.
	asks chan approval

	// pending is a turn waiting to be allowed out.
	pending *approval

	// id is what this conversation is kept under, and started is when it began.
	id      int64
	started time.Time

	// thread counts the conversations this screen has held.
	thread int
}

func newChat(theme *ui.Theme, instance string) chat {
	field := textarea.New()
	field.Placeholder = "ask about this database"
	field.ShowLineNumbers = false
	field.SetHeight(promptRows)
	field.CharLimit = 0
	ground := lipgloss.NewStyle().Background(theme.P.Surface)
	styles := textarea.DefaultStyles(true)
	styles.Focused.Base = ground
	styles.Blurred.Base = ground
	styles.Focused.Text = ground.Foreground(theme.P.Fg)
	styles.Blurred.Text = ground.Foreground(theme.P.Fg)
	styles.Focused.CursorLine = ground
	styles.Focused.EndOfBuffer = ground
	styles.Blurred.EndOfBuffer = ground
	styles.Focused.Placeholder = ground.Foreground(theme.P.Subtle)
	styles.Blurred.Placeholder = ground.Foreground(theme.P.Subtle)
	styles.Focused.Prompt = ground
	styles.Blurred.Prompt = ground
	field.SetStyles(styles)
	field.Prompt = ""
	return chat{
		theme:    theme,
		prompt:   field,
		instance: instance,
		asks:     make(chan approval),
	}
}

// readyMsg is the back-end having been made ready, or having failed to be.
type readyMsg struct {
	talk conversation
	err  error
}

// load4Talk builds the conversation and makes it ready, which for a model that
// runs here is the whole of reading it off the disk.
func (m Model) load4Talk() (Model, tea.Cmd) {
	if m.build == nil || m.assistant != nil || m.talk.loading || !m.local4Talk() {
		return m, nil
	}
	build, consent := m.build, gate{asks: m.talk.asks}
	ctx, stop := context.WithCancel(context.Background())
	m.stopLoad = stop
	m.talk.loading = true
	m.talk.trouble = ""
	m.talk.prompt.Blur()
	return m, tea.Batch(m.guard("loading the model", func() tea.Msg {
		built, err := build(consent)
		if err != nil {
			return readyMsg{err: err}
		}
		if err := built.Warm(ctx); err != nil {
			_ = built.Close()
			return readyMsg{err: err}
		}
		return readyMsg{talk: built}
	}), m.spinner.Tick)
}

// local4Talk is whether what answers runs on this machine, which is the only
// case where being ready takes long enough to be worth showing.
func (m Model) local4Talk() bool {
	instance, ok := m.session.Settings.AI.Instance(m.talk.instance)
	return ok && ai.Kind(instance.Kind) == ai.KindLocal
}

// ready takes the back-end that was made ready, and asks it what was waiting.
func (m Model) ready(msg readyMsg) (tea.Model, tea.Cmd) {
	m.talk.loading = false
	m.stopLoad = nil
	waiting := m.talk.waiting
	m.talk.waiting = ""
	if msg.err != nil {
		if !errors.Is(msg.err, context.Canceled) {
			m.talk.trouble = msg.err.Error()
		}
		return m, m.talk.prompt.Focus()
	}
	m.assistant = msg.talk
	m.talk.loaded = true
	if waiting != "" {
		asked, cmd := m.started(waiting)
		return asked, tea.Batch(cmd, asked.talk.prompt.Focus())
	}
	return m, m.talk.prompt.Focus()
}

// released lets go of the model.
func (m Model) released() (Model, tea.Cmd) {
	if m.stopLoad != nil {
		m.stopLoad()
	}
	if m.assistant == nil {
		return m, nil
	}
	err := m.assistant.Close()
	m.assistant = nil
	m.talk.loaded = false
	m.talk.loading = false
	if err != nil {
		m.talk.trouble = err.Error()
		return m, nil
	}
	return m, m.notify(m.talk.instance + " was released; the next question loads it again")
}

// started opens a turn and returns the command that reads it.
func (m Model) started(question string) (Model, tea.Cmd) {
	if m.build == nil {
		chosen, cmd := m.choosing()
		return chosen, cmd
	}
	if m.assistant == nil && m.local4Talk() {
		loading, cmd := m.load4Talk()
		if cmd != nil {
			loading.talk.waiting = question
			return loading, cmd
		}
	}
	if m.assistant == nil {
		built, err := m.build(gate{asks: m.talk.asks})
		if err != nil {
			m.talk.trouble = err.Error()
			return m, nil
		}
		m.assistant = built
		m.talk.loaded = true
	}
	ctx, stop := context.WithCancel(context.Background())
	events := make(chan agent.Event)
	failed := make(chan error, 1)
	conversing := m.assistant

	report := crash{paths: m.workspace.Setup().Store.Paths, version: m.session.Version}
	go func() {
		defer close(events)
		defer func() {
			if cause := recover(); cause != nil {
				failed <- fell("answering", cause, report.wrote("answering", cause, debug.Stack()))
			}
		}()
		failed <- conversing.Ask(ctx, question, events)
	}()

	m.talk.token++
	m.talk.busy = true
	m.talk.trouble = ""
	m.talk.exchanges = append(m.talk.exchanges, exchange{question: question})
	m.talk.prompt.SetValue("")
	m.stopAsk = stop
	m = m.pinned()

	m.talk.running = stream{events: events, approvals: m.talk.asks, failed: failed}
	return m, tea.Batch(waitForAsk(m.talk.running, m.talk.token), m.spinner.Tick)
}

// asked takes one piece of an answer and asks for the next.
func (m Model) asked(msg askEventMsg) (tea.Model, tea.Cmd) {
	if msg.token != m.talk.token {
		return m, nil
	}
	m.talk.record(msg.event)
	return m.pinned(), waitForAsk(m.talk.running, msg.token)
}

// approving puts the question of whether a turn may be sent on the screen.
func (m Model) approving(msg askApprovalMsg) (tea.Model, tea.Cmd) {
	if msg.token != m.talk.token {
		msg.request.answer <- errRefused
		return m, nil
	}
	request := msg.request
	m.talk.pending = &request
	m.talk.prompt.Blur()
	return m, nil
}

// answered lets a turn out, or refuses it, and goes back to reading the answer.
func (m Model) answered(msg askAnswerMsg) (tea.Model, tea.Cmd) {
	msg.answer <- msg.err
	m.talk.pending = nil
	if msg.token != m.talk.token {
		return m, nil
	}
	return m, tea.Batch(waitForAsk(m.talk.running, msg.token), m.talk.prompt.Focus())
}

// finished closes the turn off, whether it ended, failed or was stopped.
func (m Model) finished(msg askEndedMsg) (tea.Model, tea.Cmd) {
	if msg.token != m.talk.token {
		return m, nil
	}
	m.talk.ended(msg.err)
	m.stopAsk = nil
	return m.pinned(), tea.Batch(m.talk.prompt.Focus(), m.keep())
}

// keep writes the conversation down at the end of a turn.
func (m Model) keep() tea.Cmd {
	held, ok := m.kept()
	if !ok {
		return nil
	}
	store := m.session.Chats
	thread := m.talk.thread
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), rememberTimeout)
		defer cancel()
		id, err := store.Save(ctx, held)
		if err != nil {
			return keptMsg{err: err, thread: thread}
		}
		return keptMsg{id: id, thread: thread}
	}
}

// kept is the conversation as it would be written down, or nothing when there
// is nowhere to write it or nothing to say.
func (m Model) kept() (chats.Chat, bool) {
	if m.session.Chats == nil || m.assistant == nil {
		return chats.Chat{}, false
	}
	remembers, ok := recalls(m.assistant)
	if !ok {
		return chats.Chat{}, false
	}
	said := remembers.Messages()
	if len(said) == 0 {
		return chats.Chat{}, false
	}
	return chats.Chat{
		ID:             m.talk.id,
		ConnectionID:   m.session.Connection.Name,
		ConnectionName: m.session.Connection.Name,
		Instance:       m.talk.instance,
		Title:          chats.Title(said, titleWidth),
		StartedAt:      m.talk.started,
		Messages:       said,
	}, true
}

// titleWidth is how much of the first question a conversation is named after.
const titleWidth = 72

// keptMsg says a conversation reached the disk, and what it is called by.
type keptMsg struct {
	id     int64
	thread int
	err    error
}

func (m Model) wasKept(msg keptMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		return m, m.notify("this conversation is not being kept: " + msg.err.Error())
	}
	if msg.thread != m.talk.thread {
		return m, nil
	}
	m.talk.id = msg.id
	return m, nil
}

// scroll4Ask walks the conversation.
func (m Model) scroll4Ask(msg tea.KeyPressMsg) Model {
	rows := m.transcriptRows()
	if msg.String() == "pgup" {
		return m.rolled4Ask(-rows)
	}
	return m.rolled4Ask(rows)
}

// rolled4Ask walks the conversation by a number of rows, from a key or from a
// wheel.
func (m Model) rolled4Ask(step int) Model {
	limit := ui.MaxOffset(m.askTranscript(ui.TextWidth(m.width)), m.transcriptRows())
	m.offset = min(max(m.offset+step, 0), limit)
	m.talk.bottom = limit
	return m
}

// pinned keeps the newest words on screen.
func (m Model) pinned() Model {
	limit := ui.MaxOffset(m.askTranscript(ui.TextWidth(m.width)), m.transcriptRows())
	if m.offset >= m.talk.bottom {
		m.offset = limit
	}
	m.talk.bottom = limit
	return m
}

// askKey is what the conversation screen does with a key. The prompt gets
// everything the screen itself does not want.
func (m Model) askKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if m.talk.pending != nil {
		return m.decide(msg)
	}
	switch {
	case key.Matches(msg, m.keys.Leave):
		return m.confirmQuit()
	case key.Matches(msg, m.keys.Back):
		if m.talk.loading {
			return m.stoppedLoading(), nil
		}
		if m.talk.busy {
			return m.halted(), nil
		}
		return m.show(viewDashboard)
	case key.Matches(msg, m.keys.Switch):
		return m.choosing()
	case key.Matches(msg, m.keys.Page):
		return m.scroll4Ask(msg), nil
	case key.Matches(msg, m.keys.History):
		return m.openChats()
	case key.Matches(msg, m.keys.NewTab):
		return m.startChat()
	case key.Matches(msg, m.keys.Thinking):
		m.talk.thinking = !m.talk.thinking
		return m.pinned(), nil
	case m.shut():
		return m, nil
	case key.Matches(msg, m.keys.Choose):
		if m.build == nil {
			return m.choosing()
		}
		return m.send()
	case m.keys.opensPalette(msg, true):
		return m.openPalette()
	}
	updated, cmd := m.talk.prompt.Update(msg)
	m.talk.prompt = updated
	return m, cmd
}

// decide answers the question of whether a turn may be sent.
func (m Model) decide(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	waiting := m.talk.pending
	switch {
	case key.Matches(msg, m.keys.Choose):
		return m.answered(askAnswerMsg{answer: waiting.answer, token: m.talk.token})
	case key.Matches(msg, m.keys.Back):
		return m.answered(askAnswerMsg{answer: waiting.answer, err: errRefused, token: m.talk.token})
	}
	return m, nil
}

// send puts the question. A line that ends in a backslash is continued rather
// than sent, which is how a question longer than one line gets written without
// giving up the key that everyone expects to send it.
func (m Model) send() (tea.Model, tea.Cmd) {
	written := m.talk.prompt.Value()
	if strings.HasSuffix(written, `\`) {
		m.talk.prompt.SetValue(strings.TrimSuffix(written, `\`) + "\n")
		return m, nil
	}
	question := strings.TrimSpace(written)
	if question == "" || m.talk.busy {
		return m, nil
	}
	return m.started(question)
}

// askAbout carries a page into the conversation. The question is put in the box
// rather than sent, so that what would leave the machine is read before it does.
func (m Model) askAbout(subject string) (tea.Model, tea.Cmd) {
	m.page = nil
	opened, cmd := m.show(viewAsk)
	m = opened.(Model)
	if m.build == nil {
		return m, cmd
	}
	m.talk.prompt.SetValue(subject)
	return m, cmd
}

// stoppedLoading gives up on a model on its way into memory.
func (m Model) stoppedLoading() Model {
	if m.stopLoad != nil {
		m.stopLoad()
	}
	return m
}

// halted cancels a turn. A local model stops computing rather than being left
// to finish an answer nobody is going to read.
func (m Model) halted() Model {
	if m.stopAsk != nil {
		m.stopAsk()
	}
	return m
}

// waitForAsk reads one thing from a running turn.
func waitForAsk(running stream, token int) tea.Cmd {
	return func() tea.Msg {
		select {
		case request := <-running.approvals:
			return askApprovalMsg{request: request, token: token}
		case event, open := <-running.events:
			if !open {
				return askEndedMsg{err: <-running.failed, token: token}
			}
			return askEventMsg{event: event, token: token}
		}
	}
}

// last is the exchange being answered.
func (a *chat) last() *exchange {
	if len(a.exchanges) == 0 {
		return nil
	}
	return &a.exchanges[len(a.exchanges)-1]
}

func (a *chat) record(event agent.Event) {
	current := a.last()
	if current == nil {
		return
	}
	switch event.Kind {
	case agent.EventText:
		current.answer += event.Text
	case agent.EventReasoning:
		current.reasoning += event.Text
	case agent.EventCall:
		if event.Call != nil {
			current.steps = append(current.steps, step(*event.Call))
		}
	case agent.EventResult:
		if event.Result != nil && event.Result.Failed {
			current.steps = append(current.steps, "  "+ui.Clip(event.Result.Content, stepWidth))
		}
	case agent.EventDone:
		current.done = true
	}
}

// transcript draws a conversation that was kept, by replaying it through the
// same recorder a live one goes through.
func transcript(messages []ai.Message) []exchange {
	var held chat
	for _, message := range messages {
		if message.Role == ai.RoleUser {
			held.exchanges = append(held.exchanges, exchange{question: message.Content})
			continue
		}
		for _, event := range replayed(message) {
			held.record(event)
		}
	}
	for i := range held.exchanges {
		held.exchanges[i].done = true
	}
	return held.exchanges
}

// replayed turns one message back into the events it was drawn from, in the
// order they arrived: a model reasons, then answers, then calls something.
func replayed(message ai.Message) []agent.Event {
	if message.Role == ai.RoleTool {
		if message.Result == nil {
			return nil
		}
		return []agent.Event{{Kind: agent.EventResult, Result: message.Result}}
	}
	if message.Role != ai.RoleAssistant {
		return nil
	}
	var events []agent.Event
	if message.Reasoning != "" {
		events = append(events, agent.Event{Kind: agent.EventReasoning, Text: message.Reasoning})
	}
	if message.Content != "" {
		events = append(events, agent.Event{Kind: agent.EventText, Text: message.Content})
	}
	for i := range message.Calls {
		events = append(events, agent.Event{Kind: agent.EventCall, Call: &message.Calls[i]})
	}
	return events
}

// step is a tool call written the way a person reads it: what was called, and
// enough of what it was called with to know what was looked at.
func step(call ai.ToolCall) string {
	if len(call.Arguments) == 0 {
		return call.Name
	}
	names := make([]string, 0, len(call.Arguments))
	for name := range call.Arguments {
		names = append(names, name)
	}
	sort.Strings(names)
	written := make([]string, 0, len(names))
	for _, name := range names {
		written = append(written, fmt.Sprintf("%s: %v", name, call.Arguments[name]))
	}
	return ui.Clip(call.Name+"("+strings.Join(written, ", ")+")", stepWidth)
}

func (a *chat) ended(err error) {
	a.busy = false
	current := a.last()
	if current == nil {
		return
	}
	current.done = true
	switch {
	case err == nil:
	case errors.Is(err, context.Canceled):
		current.cancelled = true
	case errors.Is(err, errRefused):
		current.cancelled = true
		current.failed = "nothing was sent"
	default:
		current.failed = err.Error()
	}
}

// boxRows is what the box at the bottom costs: what is typed, the line under it
// that says what answers, and the border round both.
const boxRows = promptRows + 3

func (m Model) transcriptRows() int {
	return max(ui.BodyHeight(m.height)-boxRows-4, 3)
}

// askBody lays the screen out: the conversation above, the box to type in at the
// bottom, and the conversation windowed so that the box never scrolls away.
func (m Model) askBody() string {
	width := ui.FrameWidth(m.width)
	inner := ui.TextWidth(m.width)
	shown, more := ui.Window(m.askTranscript(inner), m.offset, m.transcriptRows())
	parts := []string{
		m.theme.Screen("ask", "", width),
		"",
		ui.Fit(shown, m.transcriptRows()),
		"",
		m.foot(width),
	}
	if more > 0 {
		parts[2] = ui.Fit(shown, m.transcriptRows()-1) + "\n" + m.theme.Subtle.Render(scrollHint(more))
	}
	return strings.Join(parts, "\n")
}

// foot is the box you type in, with what is answering written inside it rather
// than beside it: the eye is already there when a question is about to be typed,
// and a model named in the corner of a screen reads as a fact about the database
// instead.
func (m Model) foot(width int) string {
	inner := width - 4
	if m.talk.pending != nil {
		return m.boxed(m.asked4Permission(*m.talk.pending, inner), inner)
	}
	return m.boxed(strings.Join([]string{
		m.typed(inner),
		m.meta(inner),
	}, "\n"), inner)
}

// typed is the box itself. While a model is being read into memory, or while it
// is answering, the whole of it goes dim: the keys do nothing until that
// finishes, and a box that looks ready and is not is the same lie as a box that
// takes words and drops them.
func (m Model) typed(width int) string {
	if !m.shut() {
		return m.talk.prompt.View()
	}
	written := strings.TrimSpace(m.talk.prompt.Value())
	if written == "" {
		written = m.talk.prompt.Placeholder
	}
	lines := make([]string, 0, promptRows)
	dim := m.theme.Subtle.Background(m.theme.P.Surface)
	for i, line := range strings.Split(ui.Fit(written, promptRows), "\n") {
		if i >= promptRows {
			break
		}
		lines = append(lines, dim.Render(ui.Clip(line, width)))
	}
	return strings.Join(lines, "\n")
}

// asked4Permission is the box turned into the question the assistant is waiting
// on: what would be sent, or what would be run.
func (m Model) asked4Permission(waiting approval, inner int) string {
	if waiting.statement != "" {
		return strings.Join([]string{
			m.theme.Title.Render("run this?"),
			"",
			m.theme.Statement(waiting.statement, inner),
			"",
			m.theme.Hints(
				ui.Hint{Key: "enter", Does: "run it"},
				ui.Hint{Key: "esc", Does: "do not run it"}),
		}, "\n")
	}
	said := ""
	if waiting.outbound != nil {
		said = consentBody(*waiting.outbound)
	}
	return strings.Join([]string{
		m.theme.Title.Render("send this?"),
		"",
		m.theme.Value.Render(wrap(said, inner)),
		"",
		m.theme.Hints(
			ui.Hint{Key: "enter", Does: "send"},
			ui.Hint{Key: "esc", Does: "do not send"}),
	}, "\n")
}

// boxed draws the box: a bar down the left and a ground behind the whole of it,
// rather than a border around it.
func (m Model) boxed(content string, inner int) string {
	ground := lipgloss.NewStyle().Background(m.theme.P.Surface)
	bar := ground.Foreground(m.theme.P.Border)
	if !m.shut() && m.talk.prompt.Focused() && m.talk.pending == nil {
		bar = ground.Foreground(m.theme.P.Accent)
	}
	lines := strings.Split(content, "\n")
	drawn := make([]string, 0, len(lines)+2)
	blank := bar.Render("┃") + ui.Fill("", inner+3, m.theme.P.Surface)
	drawn = append(drawn, blank)
	for _, line := range lines {
		drawn = append(drawn, bar.Render("┃")+ui.Fill("  "+line, inner+3, m.theme.P.Surface))
	}
	return strings.Join(append(drawn, blank), "\n")
}

// meta is what is answering, written inside the box under what you type.
func (m Model) meta(width int) string {
	if m.build == nil {
		return ui.SplitLine("",
			m.theme.HintsOn(m.theme.P.Surface, ui.Hint{Key: "enter", Does: "choose what answers"}), width)
	}
	ground := m.theme.P.Surface
	said := []string{m.theme.Accent.Background(ground).Render(m.talk.instance)}
	if model := m.model4Meta(); model != "" {
		said = append(said, m.theme.Value.Background(ground).Render(model))
	}
	if m.talk.loading {
		said = append(said, m.spinner.View()+m.theme.Muted.Background(ground).Render(" loading"))
	}
	return ui.SplitLine(strings.Join(said, m.theme.Subtle.Background(ground).Render(" · ")),
		m.hint4Meta(), width)
}

// hint4Meta is the key worth offering under the box, which is the one that
// changes while a model is being loaded or held.
func (m Model) hint4Meta() string {
	ground := m.theme.P.Surface
	switch {
	case m.talk.loading:
		return m.theme.HintsOn(ground, ui.Hint{Key: "esc", Does: "stop loading"})
	case m.talk.busy:
		return m.theme.HintsOn(ground, ui.Hint{Key: "esc", Does: "cancel"})
	}
	return m.theme.HintsOn(ground, ui.Hint{Key: "ctrl+o", Does: "change"})
}

// shut is whether the box is closed to typing: while a model is being read in,
// and while it is answering.
func (m Model) shut() bool { return m.talk.loading || m.talk.busy }

// model4Meta is the model the instance answers with, which is the part somebody
// actually recognises when two instances share a provider.
func (m Model) model4Meta() string {
	instance, ok := m.session.Settings.AI.Instance(m.talk.instance)
	if !ok {
		return ""
	}
	if entry, err := local.Offered(instance.Model); err == nil {
		return entry.Title
	}
	return instance.Model
}

func (m Model) askTranscript(width int) string {
	if m.talk.trouble != "" {
		return m.theme.Error.Render("✗ " + m.talk.trouble)
	}
	if len(m.talk.exchanges) == 0 {
		return ""
	}
	blocks := make([]string, 0, len(m.talk.exchanges)*2)
	for i, said := range m.talk.exchanges {
		blocks = append(blocks, m.exchangeView(said, width, i == len(m.talk.exchanges)-1))
	}
	return strings.Join(blocks, "\n\n")
}

func (m Model) exchangeView(said exchange, width int, current bool) string {
	parts := []string{
		m.asked4View(said.question, width),
		"",
		m.theme.Accent.Render(m.talk.instance),
	}
	if thought := m.thinking4View(said, width, current); thought != "" {
		parts = append(parts, thought, "")
	}
	for _, taken := range said.steps {
		parts = append(parts, m.theme.Subtle.Render("· "+taken))
	}
	if len(said.steps) > 0 {
		parts = append(parts, "")
	}
	parts = append(parts, m.answerView(said, width, current))
	if said.failed != "" {
		parts = append(parts, m.theme.Error.Render("✗ "+said.failed))
	}
	if said.cancelled && said.failed == "" {
		parts = append(parts, m.theme.Muted.Render("stopped"))
	}
	return strings.Join(parts, "\n")
}

// asked4View is the question, drawn as the thing that was said rather than
// labelled as it.
func (m Model) asked4View(question string, width int) string {
	room := max(width-2, 1)
	body := lipgloss.NewStyle().Background(m.theme.P.Surface).Foreground(m.theme.P.OnSelection)
	bar := lipgloss.NewStyle().Background(m.theme.P.Surface).Foreground(m.theme.P.Accent)
	lines := strings.Split(wrap(strings.TrimSpace(question), room), "\n")
	drawn := make([]string, 0, len(lines))
	for _, line := range lines {
		drawn = append(drawn, bar.Render("▌ ")+body.Width(room).Render(line))
	}
	return strings.Join(drawn, "\n")
}

// thinking4View is the working a model showed.
func (m Model) thinking4View(said exchange, width int, current bool) string {
	if said.reasoning == "" {
		return ""
	}
	if m.talk.thinking {
		return m.theme.Subtle.Render("▾ thinking") + "\n" +
			m.theme.Subtle.Render(wrap(strings.TrimSpace(said.reasoning), width))
	}
	if current && m.talk.busy && said.answer == "" {
		return m.theme.Subtle.Render("▾ thinking") + "\n" +
			m.theme.Subtle.Render(wrap(tail(said.reasoning, thoughtLines), width))
	}
	return m.theme.Subtle.Render("▸ thinking · " + ui.Keystroke("ctrl+t"))
}

// tail is the last few lines of something being written, which is where the
// writing is happening.
func tail(text string, lines int) string {
	written := strings.Split(strings.TrimRight(text, "\n"), "\n")
	if len(written) <= lines {
		return strings.TrimSpace(text)
	}
	return strings.Join(written[len(written)-lines:], "\n")
}

// thoughtLines is how much of the working is shown while it is the only thing
// there is to show.
const thoughtLines = 3

// answerView draws an answer that is still arriving as plain text and one that
// has finished as markdown.
func (m Model) answerView(said exchange, width int, current bool) string {
	if said.answer == "" {
		if current && m.talk.busy {
			return m.spinner.View() + m.theme.Muted.Render(" thinking")
		}
		return m.theme.Muted.Render("nothing came back")
	}
	if !said.done {
		return m.theme.Value.Render(said.answer)
	}
	return strings.TrimRight(m.theme.Markdown(width).Render(said.answer), "\n")
}

// consentBody is the panel that says what would be sent and asks whether to send
// it.
func consentBody(outbound agent.Outbound) string {
	classes := make([]string, 0, len(outbound.Classes))
	for _, class := range outbound.Classes {
		classes = append(classes, string(class))
	}
	sort.Strings(classes)
	return fmt.Sprintf("%s would be sent to %s, which runs on somebody else's machine.\n\nThis turn adds: %s.",
		size(outbound.Bytes), outbound.Model, strings.Join(classes, ", "))
}

func size(value int) string {
	if value < 1024 {
		return fmt.Sprintf("%d bytes", value)
	}
	return fmt.Sprintf("%.0f KiB", float64(value)/1024)
}

// releaseMsg is the command that gives back the memory a local model is loaded
// into.
type releaseMsg struct{}
