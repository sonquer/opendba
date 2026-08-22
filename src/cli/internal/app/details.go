package app

import (
	"embed"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/sonquer/tui4db/src/cli/internal/driver"
	"github.com/sonquer/tui4db/src/cli/internal/ui"
)

//go:embed findings/*.md
var pages embed.FS

// explain returns the page written for a finding code, or nothing when there is
// no page for it and the driver's own note has to do.
func explain(code string) string {
	page, err := pages.ReadFile("findings/" + code + ".md")
	if err != nil {
		return ""
	}
	return string(page)
}

type pair struct {
	key   string
	value string
}

// state is the one word that says what a session is doing. It carries the
// accent while the session is working, because that is the reason the page was
// opened, and stays quiet once it is only holding a connection.
func (m Model) state(session driver.Session) string {
	if session.Running() {
		return m.theme.Accent.Render(session.State)
	}
	return m.theme.Muted.Render(session.State)
}

// details is a page about the row under the cursor. It is read rather than
// answered, which is what separates it from modal.
type details struct {
	theme  *ui.Theme
	title  string
	tag    string
	pairs  []pair
	meter  *ui.Reading
	prose  string
	code   string
	offset int
}

func (d details) scroll(step int) details {
	d.offset = max(d.offset+step, 0)
	return d
}

func (d details) view(width, height int) string {
	inner := min(ui.TextWidth(width)-6, 88)
	rows := max(height-10, 6)
	body := []string{}
	if d.meter != nil {
		body = append(body, d.theme.Value.Render(d.meter.Value), d.theme.Meter(*d.meter, inner), "")
	}
	if len(d.pairs) > 0 {
		body = append(body, d.pairsView(inner), "")
	}
	if d.prose != "" {
		body = append(body, d.theme.Markdown(inner).Render(d.prose))
	}
	if d.code != "" {
		body = append(body, d.theme.Statement(d.code, inner))
	}
	shown, more := ui.Window(strings.Join(body, "\n"), d.offset, rows)
	head := ui.SplitLine(d.theme.Title.Render(ui.Truncate(d.title, inner-12)), d.tag, inner)
	foot := ui.Dotted(ui.Keystroke("esc")+" back", "↑↓ scroll")
	if more > 0 {
		foot = ui.Dotted(foot, ui.Plural(more, "more line", "more lines"))
	}
	return d.theme.Panel.Render(square(strings.Join([]string{
		head, d.theme.Rule(inner), "", shown, "", d.theme.Subtle.Render(foot),
	}, "\n"), inner))
}

// fill squares the page off: every line the same width, so the rule under the
// title reaches the border and the panel cannot be widened by one long line
// that a renderer measured differently.
func square(page string, width int) string {
	lines := strings.Split(page, "\n")
	for i, line := range lines {
		lines[i] = ui.Pad(line, width)
	}
	return strings.Join(lines, "\n")
}

func (d details) pairsView(width int) string {
	label := 0
	for _, row := range d.pairs {
		label = max(label, lipgloss.Width(row.key))
	}
	lines := make([]string, 0, len(d.pairs))
	for _, row := range d.pairs {
		lines = append(lines, d.theme.Label.Render(ui.Pad(row.key, label))+"  "+
			d.theme.Value.Render(ui.Truncate(row.value, width-label-2)))
	}
	return strings.Join(lines, "\n")
}

func (m Model) pageKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.Back), key.Matches(msg, m.keys.Choose):
		m.page = nil
	case key.Matches(msg, m.keys.Up):
		scrolled := m.page.scroll(-1)
		m.page = &scrolled
	case key.Matches(msg, m.keys.Down):
		scrolled := m.page.scroll(1)
		m.page = &scrolled
	case key.Matches(msg, m.keys.Quit), key.Matches(msg, m.keys.Leave):
		return m.confirmQuit()
	}
	return m, nil
}

// readingPage is what a reading says when there is room to say it: the number,
// where it sits on its scale, and the page written for that check.
func (m Model) readingPage() (tea.Model, tea.Cmd) {
	readings := m.readings(every)
	if m.reading < 0 || m.reading >= len(readings) {
		return m, nil
	}
	chosen := readings[m.reading]
	prose := explain(chosen.Code)
	if prose == "" {
		prose = chosen.Note
	}
	page := details{
		theme: m.theme,
		title: chosen.Label,
		tag:   m.theme.Severity(chosen.Severity).Render(ui.Verdict(chosen.Severity)),
		meter: &chosen,
		prose: prose,
	}
	if !chosen.Measured {
		page.meter = nil
		page.pairs = []pair{{key: "reading", value: chosen.Value}}
	}
	m.page = &page
	return m, nil
}

// sessionPage is everything about one session, including the statement that
// would not fit on its row.
func (m Model) sessionPage() (tea.Model, tea.Cmd) {
	chosen, ok := m.running.selected()
	if !ok {
		return m, nil
	}
	page := details{
		theme: m.theme,
		title: "session " + chosen.ID,
		tag:   m.state(chosen),
		pairs: []pair{
			{key: "user", value: chosen.User},
			{key: "application", value: or(chosen.Application, "—")},
			{key: "client", value: chosen.Client},
			{key: "database", value: chosen.Database},
			{key: "waiting on", value: or(chosen.Wait, "nothing")},
			{key: "for", value: driver.Duration(chosen.Duration)},
		},
		code: chosen.Statement,
	}
	m.page = &page
	return m, nil
}
