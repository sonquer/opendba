package app

import (
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/sonquer/tui4db/src/cli/internal/ui"
)

const modalWidth = 52

// modal is a question the program will not answer for you. It carries the
// message to send once the answer is yes.
type modal struct {
	theme   *ui.Theme
	title   string
	body    string
	confirm string
	input   textinput.Model
	action  tea.Msg
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
	return d.confirm == "" || strings.TrimSpace(d.input.Value()) == d.confirm
}

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
	lines := []string{d.theme.Title.Render(d.title)}
	if d.body != "" {
		lines = append(lines, "", d.theme.Muted.Render(ui.Truncate(d.body, inner)))
	}
	if d.typing() {
		lines = append(lines, "", d.theme.Prompt.Render("› ")+d.input.View())
	}
	lines = append(lines, "", d.theme.Subtle.Render(ui.Dotted(
		ui.Keystroke("enter")+" yes", ui.Keystroke("esc")+" no")))
	return d.theme.Panel.Width(inner).Render(strings.Join(lines, "\n"))
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
	if !m.modal.typing() {
		return m, nil
	}
	dialog := *m.modal
	cmd := dialog.edit(msg)
	m.modal = &dialog
	return m, cmd
}
