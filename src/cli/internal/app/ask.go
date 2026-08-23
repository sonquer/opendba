package app

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/sonquer/tui4db/src/cli/internal/ai"
	"github.com/sonquer/tui4db/src/cli/internal/ai/agent"
	"github.com/sonquer/tui4db/src/cli/internal/ai/providers/local"
	"github.com/sonquer/tui4db/src/cli/internal/ui"
)

const (
	promptRows = 3

	// stepWidth is how much of a tool call is shown on the line that says one
	// was made. The arguments are there to tell you what was looked at, not to
	// be read in full.
	stepWidth = 64
)

// errRefused is what the assistant is told when a turn is not allowed to leave
// the machine.
var errRefused = errors.New("you did not allow this to be sent")

// conversation is what the Ask screen talks to. It is an interface rather than
// the agent itself so that the screen can be driven in a test by something that
// answers without a model, a key or a network.
type conversation interface {
	Ask(ctx context.Context, question string, out chan<- agent.Event) error

	// Warm makes the back-end ready before a question rather than during one.
	// A model that runs here is gigabytes off a disk, and the wait belongs
	// where it can be seen rather than in the middle of an answer.
	Warm(ctx context.Context) error

	// Close lets go of what the back-end is holding, which for a model running
	// here is the memory it was loaded into.
	Close() error
}

// approval is a question the assistant asks the screen and waits for. The
// assistant runs in a goroutine, so consent has to travel out as a message and
// come back as one.
type approval struct {
	outbound agent.Outbound
	answer   chan error
}

// gate is what the assistant asks before a turn leaves the machine.
type gate struct{ asks chan approval }

// Allow puts the question to the screen and waits for the person to answer it.
func (g gate) Allow(ctx context.Context, outbound agent.Outbound) error {
	answer := make(chan error, 1)
	select {
	case g.asks <- approval{outbound: outbound, answer: answer}:
	case <-ctx.Done():
		return ctx.Err()
	}
	select {
	case err := <-answer:
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

// exchange is one question and what came back. The answer is kept as plain text
// while it is arriving and rendered as markdown once it is finished, because a
// code fence that has been opened and not yet closed is not markdown yet.
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

	// loading is a model on its way into memory. The box is not typed into
	// while it is true: a question written now would sit there until the load
	// finished anyway, and a box that takes words and does nothing with them
	// reads as a program that has stopped.
	loading bool

	// loaded is a back-end that is ready, which is what makes letting go of it
	// something there is any point offering.
	loaded bool

	// thinking is whether the reasoning a model shows its working in is opened
	// out. It is one setting for the whole conversation rather than one per
	// answer: somebody either wants to see the working or does not.
	thinking bool

	// asks is how the assistant reaches the person for permission. It belongs
	// to the screen rather than to the assistant, because the screen is what
	// can put the question somewhere it will be read.
	asks chan approval

	// pending is a turn waiting to be allowed out. The assistant is blocked in
	// a goroutine until it is answered, which is why refusing has to send an
	// answer rather than simply forgetting the question.
	pending *approval
}

func newChat(theme *ui.Theme, instance string) chat {
	field := textarea.New()
	field.Placeholder = "ask about this database"
	field.ShowLineNumbers = false
	field.SetHeight(promptRows)
	field.CharLimit = 0
	styles := textarea.DefaultStyles(true)
	styles.Focused.Base = lipgloss.NewStyle()
	styles.Blurred.Base = lipgloss.NewStyle()
	styles.Focused.CursorLine = lipgloss.NewStyle()
	styles.Focused.Placeholder = lipgloss.NewStyle().Foreground(theme.P.Subtle)
	styles.Focused.Prompt = lipgloss.NewStyle().Foreground(theme.P.Accent)
	styles.Blurred.Prompt = lipgloss.NewStyle().Foreground(theme.P.Border)
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

// loading builds the conversation and makes it ready, which for a model that
// runs here is the whole of reading it off the disk.
//
// It happens when a model is chosen and when the screen is opened, not when the
// first question is asked: the wait is the same either way, and this is the one
// place where it is something visibly happening rather than a program that has
// stopped answering.
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
	return m, tea.Batch(func() tea.Msg {
		built, err := build(consent)
		if err != nil {
			return readyMsg{err: err}
		}
		if err := built.Warm(ctx); err != nil {
			_ = built.Close()
			return readyMsg{err: err}
		}
		return readyMsg{talk: built}
	}, m.spinner.Tick)
}

// local4Talk is whether what answers runs on this machine, which is the only
// case where being ready takes long enough to be worth showing. A back-end
// somewhere else is opened when the first question is asked, because opening it
// is building a struct.
func (m Model) local4Talk() bool {
	instance, ok := m.session.Settings.AI.Instance(m.talk.instance)
	return ok && ai.Kind(instance.Kind) == ai.KindLocal
}

// ready takes the back-end that was made ready, and hands the box back.
func (m Model) ready(msg readyMsg) (tea.Model, tea.Cmd) {
	m.talk.loading = false
	m.stopLoad = nil
	if msg.err != nil {
		if errors.Is(msg.err, context.Canceled) {
			return m, m.talk.prompt.Focus()
		}
		m.talk.trouble = msg.err.Error()
		return m, m.talk.prompt.Focus()
	}
	m.assistant = msg.talk
	m.talk.loaded = true
	return m, m.talk.prompt.Focus()
}

// released lets go of the model. It is the other half of loading one: a model
// that runs here holds gigabytes for as long as it is loaded, and somebody who
// is done asking questions should be able to have them back without leaving the
// program.
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

	go func() {
		defer close(events)
		failed <- conversing.Ask(ctx, question, events)
	}()

	m.talk.token++
	m.talk.busy = true
	m.talk.trouble = ""
	m.talk.exchanges = append(m.talk.exchanges, exchange{question: question})
	m.talk.prompt.SetValue("")
	m.stopAsk = stop
	m.offset = ui.MaxOffset(m.askTranscript(ui.TextWidth(m.width)), m.transcriptRows())

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

// approving puts the question of whether a turn may be sent on the screen. The
// assistant is waiting on the answer, so a turn that is no longer the current
// one is refused rather than left hanging.
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
	return m.pinned(), m.talk.prompt.Focus()
}

// scroll4Ask walks the conversation. It cannot use the scrolling every other
// screen shares, because that one measures the whole body against the window
// and this body is a fixed height: the transcript has a window of its own and
// is the only part of it that moves.
func (m Model) scroll4Ask(msg tea.KeyPressMsg) Model {
	rows := m.transcriptRows()
	step := rows
	if msg.String() == "pgup" {
		step = -rows
	}
	limit := ui.MaxOffset(m.askTranscript(ui.TextWidth(m.width)), rows)
	m.offset = min(max(m.offset+step, 0), limit)
	return m
}

// pinned keeps the newest words on screen. Someone who has scrolled back is
// left where they are, because yanking the view away mid-read is worse than
// missing the last line.
func (m Model) pinned() Model {
	width := ui.TextWidth(m.width)
	m.offset = ui.MaxOffset(m.askTranscript(width), m.transcriptRows())
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
	case m.talk.loading:
		return m, nil
	case key.Matches(msg, m.keys.Release):
		return m.released()
	case key.Matches(msg, m.keys.Thinking):
		m.talk.thinking = !m.talk.thinking
		return m.pinned(), nil
	case key.Matches(msg, m.keys.Choose):
		if m.build == nil {
			return m.choosing()
		}
		return m.send()
	case m.keys.opensPalette(msg, true):
		return m.openPalette()
	case key.Matches(msg, m.keys.Page):
		return m.scroll4Ask(msg), nil
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

// waitForAsk reads one thing from a running turn. It is issued again for every
// piece, which is how a blocking read becomes something a screen can wait on
// while staying answerable.
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
	return max(ui.BodyHeight(m.height)-boxRows-3, 3)
}

// askBody lays the screen out: the conversation above, the box to type in at
// the bottom, and the conversation windowed so that the box never scrolls away.
//
// The transcript is padded to the room it has rather than left to be as tall as
// it happens to be, which is what keeps the box on the last line of the screen
// instead of floating up under a short answer.
func (m Model) askBody() string {
	width := ui.FrameWidth(m.width)
	inner := ui.TextWidth(m.width)
	shown, more := ui.Window(m.askTranscript(inner), m.offset, m.transcriptRows())
	parts := []string{
		m.theme.Screen("ask", "", width),
		"",
		ui.Fit(shown, m.transcriptRows()),
		m.foot(width),
	}
	if more > 0 {
		parts[2] = ui.Fit(shown, m.transcriptRows()-1) + "\n" + m.theme.Subtle.Render(scrollHint(more))
	}
	return strings.Join(parts, "\n")
}

// foot is the box you type in, with what is answering written inside it rather
// than beside it: the eye is already there when a question is about to be
// typed, and a model named in the corner of a screen reads as a fact about the
// database instead.
//
// The border is the box. It takes the accent colour while the box has the keys
// and the plain border colour when something else does, which is the only place
// on this screen that says where typing goes.
func (m Model) foot(width int) string {
	inner := width - 4
	if m.talk.pending != nil {
		return m.boxed(strings.Join([]string{
			m.theme.Title.Render("send this?"),
			"",
			m.theme.Value.Render(wrap(consentBody(m.talk.pending.outbound), inner)),
			"",
			m.theme.Hints(
				ui.Hint{Key: "enter", Does: "send"},
				ui.Hint{Key: "esc", Does: "do not send"}),
		}, "\n"), inner)
	}
	return m.boxed(strings.Join([]string{
		m.talk.prompt.View(),
		m.meta(inner),
	}, "\n"), inner)
}

// boxed puts the border round the box, squaring the contents off first so that
// the border is exactly as wide as the screen and nothing inside it wraps.
func (m Model) boxed(content string, inner int) string {
	panel := m.theme.Panel
	if m.talk.prompt.Focused() && m.talk.pending == nil {
		panel = panel.BorderForeground(m.theme.P.Accent)
	}
	return panel.Render(square(content, inner))
}

// meta is what is answering, written inside the box under what you type.
func (m Model) meta(width int) string {
	if m.build == nil {
		return ui.SplitLine("",
			m.theme.Hints(ui.Hint{Key: "enter", Does: "choose what answers"}), width)
	}
	said := []string{m.theme.Accent.Render(m.talk.instance)}
	if model := m.model4Meta(); model != "" {
		said = append(said, m.theme.Value.Render(model))
	}
	switch {
	case m.talk.loading:
		said = append(said, m.spinner.View()+m.theme.Muted.Render(" loading"))
	case m.talk.busy:
		said = append(said, m.spinner.View()+m.theme.Muted.Render(" thinking"))
	}
	return ui.SplitLine(ui.Dotted(said...), m.hint4Meta(), width)
}

// hint4Meta is the key worth offering under the box, which is the one that
// changes while a model is being loaded or held.
func (m Model) hint4Meta() string {
	if m.talk.loading {
		return m.theme.Hints(ui.Hint{Key: "esc", Does: "stop loading"})
	}
	if m.talk.loaded {
		return m.theme.Hints(ui.Hint{Key: "u", Does: "release"}, ui.Hint{Key: "ctrl+o", Does: "change"})
	}
	return m.theme.Hints(ui.Hint{Key: "ctrl+o", Does: "change"})
}

// model4Meta is the model the instance answers with, which is the part somebody
// actually recognises when two instances share a provider.
//
// A model that runs here is named the way the catalogue names it, because
// "Gemma 4 E4B" is what it is called everywhere else in this program and
// gemma-4-e4b-qat is a directory on a disk.
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
		m.theme.Label.Render("you"),
		m.theme.Value.Render(said.question),
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

// thinking4View is the working a model showed. It is folded away by default and
// opened with a key, because it is worth having and is not the answer: three
// paragraphs of deliberation above two lines of reply would bury the reply.
//
// While it is the only thing that has arrived it is shown regardless, as its
// last few lines. A model that has been reasoning for thirty seconds and shown
// nothing is indistinguishable from one that has hung.
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
// has finished as markdown. Rendering markdown that is half written puts an
// unterminated code fence through a parser that has every right to refuse it.
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

// consentBody is the panel that says what would be sent and asks whether to
// send it. It names classes rather than bytes, because that is the question
// somebody can actually answer.
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
