package app

import (
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/sonquer/opendba/src/cli/internal/ui"
)

const (
	// toastLife is how long one sentence stays on screen.
	toastLife = 3 * time.Second

	// toastsShown is how many are drawn at once.
	toastsShown = 3

	// toastWidth is the widest a sentence is drawn before it wraps, and toastFrame
	// is what the ground around it adds: a bar, and the room on either side of the
	// words.
	toastWidth = 44
	toastFrame = 5
)

type toastMsg struct{ sequence int }

// note is one thing the program has said, and whether it was a complaint.
type note struct {
	text     string
	alarm    bool
	sequence int
}

// toaster is what the program says in passing, stacked in the top right.
type toaster struct {
	notes    []note
	sequence int
}

func (t *toaster) notify(text string) tea.Cmd { return t.say(text, false) }

// alarm is the same thing said about something that went wrong, drawn in the
// colour of a problem rather than the colour of news.
func (t *toaster) alarm(text string) tea.Cmd { return t.say(text, true) }

func (t *toaster) say(text string, alarm bool) tea.Cmd {
	t.sequence++
	sequence := t.sequence
	t.notes = append(t.notes, note{text: text, alarm: alarm, sequence: sequence})
	if len(t.notes) > toastsShown {
		t.notes = t.notes[len(t.notes)-toastsShown:]
	}
	return tea.Tick(toastLife, func(time.Time) tea.Msg {
		return toastMsg{sequence: sequence}
	})
}

func (t *toaster) expire(msg toastMsg) {
	kept := t.notes[:0]
	for _, held := range t.notes {
		if held.sequence != msg.sequence {
			kept = append(kept, held)
		}
	}
	t.notes = kept
}

// text is the newest thing said, which is what a test asks about and what a
// screen with room for one line would show.
func (t toaster) text() string {
	if len(t.notes) == 0 {
		return ""
	}
	return t.notes[len(t.notes)-1].text
}

// render draws the stack, newest at the top, each on a ground of its own with a
// bar in the colour of what it is about.
func (t toaster) render(theme *ui.Theme) string {
	if len(t.notes) == 0 {
		return ""
	}
	drawn := make([]string, 0, len(t.notes)*2)
	for i := len(t.notes) - 1; i >= 0; i-- {
		if len(drawn) > 0 {
			drawn = append(drawn, "")
		}
		drawn = append(drawn, t.one(theme, t.notes[i]))
	}
	return lipgloss.JoinVertical(lipgloss.Right, drawn...)
}

// one draws a single sentence: the words on their ground with room above and
// below them, and a bar down the left in the colour of what it is about.
func (t toaster) one(theme *ui.Theme, held note) string {
	lines := strings.Split(lipgloss.NewStyle().Width(toastWidth).Render(held.text), "\n")
	widest := 0
	for i, line := range lines {
		lines[i] = strings.TrimRight(line, " ")
		if width := lipgloss.Width(lines[i]); width > widest {
			widest = width
		}
	}
	for i, line := range lines {
		lines[i] = ui.Pad(line, widest)
	}
	said := theme.Toast.Render(strings.Join(lines, "\n"))
	colour := theme.P.Accent
	if held.alarm {
		colour = theme.P.Critical
	}
	bar := lipgloss.NewStyle().Foreground(colour).Background(theme.P.Surface).
		Render(strings.Repeat("▌\n", lipgloss.Height(said)-1) + "▌")
	return lipgloss.JoinHorizontal(lipgloss.Top, bar, said)
}
