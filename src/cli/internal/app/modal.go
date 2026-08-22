package app

import (
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/sonquer/tui4db/src/cli/internal/ui"
)

const modalWidth = 52

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

func (d modal) ready() bool {
	if d.needs != "" && !d.ticked {
		return false
	}
	return d.confirm == "" || strings.TrimSpace(d.input.Value()) == d.confirm
}

// warning turns a refusal into a question with a way through: the danger is
// spelled out, and the way through is a box that has to be ticked on purpose.
func (d *modal) warning(text, tick string) {
	d.danger, d.warn, d.needs = true, text, tick
}

func (d *modal) toggle() { d.ticked = !d.ticked }

func (d modal) typing() bool { return d.confirm != "" }

func (d *modal) edit(msg tea.KeyPressMsg) tea.Cmd {
	updated, cmd := d.input.Update(msg)
	d.input = updated
	return cmd
}

func (d modal) view(width int) string {
	inner := modalWidth
	if room := ui.TextWidth(width) - 6; room < inner {
		inner = room
	}
	d.input.SetWidth(inner - 4)
	title := d.theme.Title.Render(d.title)
	if d.danger {
		title = d.theme.Severity(ui.SevCritical).Bold(true).Render(d.title)
	}
	lines := []string{ui.SplitLine(title, d.theme.Muted.Render(d.tag), inner)}
	if d.body != "" {
		lines = append(lines, "", d.theme.Muted.Render(wrap(d.body, inner)))
	}
	if d.warn != "" {
		lines = append(lines, "", d.theme.Severity(ui.SevCritical).
			Render("⚠ "+wrap(d.warn, inner-2)))
	}
	if d.needs != "" {
		lines = append(lines, "", d.tick()+" "+d.theme.Value.Render(d.needs))
	}
	if d.typing() {
		lines = append(lines, "", d.theme.Prompt.Render("› ")+d.input.View())
	}
	hints := []string{ui.Keystroke("enter") + " yes", ui.Keystroke("esc") + " no"}
	if d.needs != "" {
		hints = append([]string{ui.Keystroke("space") + " ticks the box"}, hints...)
	}
	lines = append(lines, "", d.theme.Subtle.Render(ui.Dotted(hints...)))
	panel := d.theme.Panel
	if d.danger {
		panel = panel.BorderForeground(d.theme.P.Critical)
	}
	return panel.Width(inner).Render(strings.Join(lines, "\n"))
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
	m.modal = ask(m.theme, "close tui4db?",
		"the connection is closed with it", quitMsg{})
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
		answered := m.modal.action
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
