package app

import (
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/sonquer/tui4db/src/cli/internal/ui"
)

const toastLife = 3 * time.Second

type toastMsg struct{ sequence int }

type toaster struct {
	text     string
	sequence int
}

func (t *toaster) notify(text string) tea.Cmd {
	t.sequence++
	t.text = text
	sequence := t.sequence
	return tea.Tick(toastLife, func(time.Time) tea.Msg { return toastMsg{sequence: sequence} })
}

func (t *toaster) expire(msg toastMsg) {
	if msg.sequence == t.sequence {
		t.text = ""
	}
}

func (t toaster) render(theme *ui.Theme) string {
	if t.text == "" {
		return ""
	}
	return theme.Severity(ui.SevInfo).Render("· " + t.text)
}
