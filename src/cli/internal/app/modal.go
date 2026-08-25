package app

import (
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/sonquer/opendba/src/cli/internal/ui"
)

const (
	modalWidth = 52

	// statementWidth is what a dialog needs when it is showing the statement it is
	// about, because a statement wrapped at fifty columns is a statement nobody can
	// read before answering a question about it.
	statementWidth = 76
)

// modal is a question the program will not answer for you. It carries the
// message to send once the answer is yes.
type modal struct {
	theme   *ui.Theme
	title   string
	tag     string
	body    string
	confirm string
	input   textinput.Model
	action  tea.Msg

	danger bool
	warn   string
	ticked bool
	needs  string

	// code is the statement the dialog is about, drawn the way a statement is drawn
	// everywhere else: highlighted, with the line numbers a person points at when
	// they talk about it.
	code string

	// chart is a picture of the numbers the question turns on, drawn above the
	// sentence rather than instead of it.
	chart string

	// reply turns what was typed into the message the answer sends, for a
	// dialog that asks for a name rather than for a yes.
	reply func(string) tea.Msg
}

func ask(theme *ui.Theme, title, body string, action tea.Msg) *modal {
	return &modal{theme: theme, title: title, body: body, action: action}
}

// askTyped raises a question that is only answered by typing the word back,
// which is what removing something with a password behind it deserves.
func askTyped(theme *ui.Theme, title, body, word string, action tea.Msg) (*modal, tea.Cmd) {
	dialog := ask(theme, title, body, action)
	dialog.confirm = word
	dialog.input = input(theme, "", false)
	return dialog, dialog.input.Focus()
}

// askName raises a question answered by typing a name, which is what saving
// something that has never had one needs.
func askName(theme *ui.Theme, title, body string, reply func(string) tea.Msg) (*modal, tea.Cmd) {
	dialog := ask(theme, title, body, nil)
	dialog.reply = reply
	dialog.input = input(theme, "", false)
	return dialog, dialog.input.Focus()
}

func (d modal) ready() bool {
	if d.needs != "" && !d.ticked {
		return false
	}
	if d.reply != nil {
		return strings.TrimSpace(d.input.Value()) != ""
	}
	return d.confirm == "" || strings.TrimSpace(d.input.Value()) == d.confirm
}

// answer is the message the dialog sends once it is answered.
func (d modal) answer() tea.Msg {
	if d.reply != nil {
		return d.reply(strings.TrimSpace(d.input.Value()))
	}
	return d.action
}

// warning turns a refusal into a question with a way through: the danger is
// spelled out, and the way through is a box that has to be ticked on purpose.
func (d *modal) warning(text, tick string) {
	d.danger, d.warn, d.needs = true, text, tick
}

func (d *modal) toggle() { d.ticked = !d.ticked }

func (d modal) typing() bool { return d.confirm != "" || d.reply != nil }

func (d *modal) edit(msg tea.KeyPressMsg) tea.Cmd {
	updated, cmd := d.input.Update(msg)
	d.input = updated
	return cmd
}

// view draws the dialog. The lines are built to fit inside the border and its
// padding, then squared off, so the panel is exactly as wide as it was asked to
// be and nothing inside it wraps.
func (d modal) view(width int) string {
	outer := modalWidth
	if d.code != "" {
		outer = statementWidth
	}
	if room := ui.TextWidth(width) - 6; room < outer {
		outer = room
	}
	inner := outer - 4
	d.input.SetWidth(inner - 4)
	title := d.theme.Title.Render(d.title)
	if d.danger {
		title = d.theme.Severity(ui.SevCritical).Bold(true).Render(d.title)
	}
	lines := []string{ui.SplitLine(title, d.tag, inner)}
	if d.chart != "" {
		lines = append(lines, "", d.chart)
	}
	if d.body != "" {
		lines = append(lines, "", d.theme.Muted.Render(wrap(d.body, inner)))
	}
	if d.warn != "" {
		lines = append(lines, "", d.theme.Severity(ui.SevCritical).
			Render("⚠ "+wrap(d.warn, inner-2)))
	}
	if d.code != "" {
		lines = append(lines, "", d.theme.Statement(d.code, inner))
	}
	if d.needs != "" {
		lines = append(lines, "", d.tick()+" "+d.theme.Value.Render(d.needs))
	}
	if d.typing() {
		lines = append(lines, "", d.theme.Prompt.Render("› ")+d.input.View())
	}
	hints := []ui.Hint{{Key: "enter", Does: "yes"}, {Key: "esc", Does: "no"}}
	if d.needs != "" {
		hints = append([]ui.Hint{{Key: "space", Does: "ticks the box"}}, hints...)
	}
	lines = append(lines, "", d.theme.Hints(hints...))
	panel := d.theme.Panel
	if d.danger {
		panel = panel.BorderForeground(d.theme.P.Critical)
	}
	return panel.Render(square(strings.Join(lines, "\n"), inner))
}

func (d modal) tick() string {
	if d.ticked {
		return d.theme.Accent.Render("▣")
	}
	return d.theme.Subtle.Render("▢")
}

// wrap breaks a sentence at the width it has, because a dialog that scrolls is
// a dialog nobody reads.
func wrap(text string, width int) string {
	return lipgloss.NewStyle().Width(width).Render(text)
}

func (m Model) confirmQuit() (tea.Model, tea.Cmd) {
	dialog := ask(m.theme, "close opendba?",
		"the connection is closed with it", quitMsg{})
	if waiting := m.running4Tabs(); waiting > 0 {
		dialog.tag = m.theme.Muted.Render(
			ui.Plural(waiting, "statement", "statements") + " still running")
	}
	m.modal = dialog
	return m, nil
}

func (m Model) modalKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.Leave):
		m.quitting = true
		return m, tea.Quit
	case key.Matches(msg, m.keys.Back):
		m.modal = nil
		return m, nil
	case key.Matches(msg, m.keys.Choose):
		if !m.modal.ready() {
			return m, nil
		}
		answered := m.modal.answer()
		m.modal = nil
		return m, func() tea.Msg { return answered }
	}
	if key.Matches(msg, m.keys.Expand) && m.modal.needs != "" {
		dialog := *m.modal
		dialog.toggle()
		m.modal = &dialog
		return m, nil
	}
	if !m.modal.typing() {
		return m, nil
	}
	dialog := *m.modal
	cmd := dialog.edit(msg)
	m.modal = &dialog
	return m, cmd
}
