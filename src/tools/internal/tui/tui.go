package tui

import (
	"context"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/sonquer/tui4db/src/tools/internal/core"
	"github.com/sonquer/tui4db/src/tools/internal/render"
)

type startedMsg struct{ index int }

type finishedMsg struct {
	index  int
	report core.Report
	err    error
}

type Model struct {
	suite   core.Suite
	theme   render.Theme
	title   string
	reports []core.Report
	queue   []int
	cursor  int
	active  int
	width   int
	height  int
	done    bool
}

func New(suite core.Suite, theme render.Theme, title string) Model {
	reports := make([]core.Report, len(suite))
	for i, check := range suite {
		reports[i] = core.Report{Check: check.Name(), Status: core.StatusPending}
	}
	return Model{
		suite:   suite,
		theme:   theme,
		title:   title,
		reports: reports,
		active:  -1,
		width:   96,
		height:  32,
	}
}

func (m Model) Reports() []core.Report { return m.reports }

func (m Model) Init() tea.Cmd { return nil }

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil
	case tea.KeyPressMsg:
		return m.handleKey(msg.String())
	case startedMsg:
		m.active = msg.index
		m.reports[msg.index].Status = core.StatusRunning
		return m, m.execute(msg.index)
	case finishedMsg:
		return m.finish(msg)
	}
	return m, nil
}

func (m Model) handleKey(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "q", "ctrl+c", "esc":
		m.done = true
		return m, tea.Quit
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
	case "down", "j":
		if m.cursor < len(m.suite)-1 {
			m.cursor++
		}
	case "enter", "r":
		return m.start([]int{m.cursor})
	case "a":
		all := make([]int, len(m.suite))
		for i := range all {
			all[i] = i
		}
		return m.start(all)
	}
	return m, nil
}

func (m Model) start(indexes []int) (tea.Model, tea.Cmd) {
	if m.active >= 0 || len(indexes) == 0 {
		return m, nil
	}
	m.queue = indexes[1:]
	return m, m.run(indexes[0])
}

func (m Model) run(index int) tea.Cmd {
	return func() tea.Msg { return startedMsg{index: index} }
}

func (m Model) execute(index int) tea.Cmd {
	check := m.suite[index]
	return func() tea.Msg {
		report, err := check.Run(context.Background())
		return finishedMsg{index: index, report: report, err: err}
	}
}

func (m Model) finish(msg finishedMsg) (tea.Model, tea.Cmd) {
	report := msg.report
	if msg.err != nil {
		report = core.Report{Check: m.suite[msg.index].Name(), Status: core.StatusFail, Summary: msg.err.Error()}
	}
	m.reports[msg.index] = report
	m.active = -1
	if len(m.queue) == 0 {
		return m, nil
	}
	next := m.queue[0]
	m.queue = m.queue[1:]
	return m, m.run(next)
}

func (m Model) View() tea.View {
	var view tea.View
	view.AltScreen = true
	view.WindowTitle = m.title
	view.SetContent(m.content())
	return view
}

func (m Model) content() string {
	muted := lipgloss.NewStyle().Foreground(m.theme.Muted)
	var b strings.Builder
	b.WriteString(lipgloss.NewStyle().Foreground(m.theme.Fg).Bold(true).Render(m.title) + "\n")
	b.WriteString(muted.Render(strings.Repeat("─", clamp(m.width, 8, 120))) + "\n\n")

	width := 0
	for _, r := range m.reports {
		if n := lipgloss.Width(r.Check); n > width {
			width = n
		}
	}
	for i, report := range m.reports {
		marker := "  "
		if i == m.cursor {
			marker = lipgloss.NewStyle().Foreground(m.theme.Accent).Render("▸ ")
		}
		b.WriteString(marker + m.theme.Line(report, width) + "\n")
	}

	b.WriteString("\n")
	selected := m.reports[m.cursor]
	b.WriteString(muted.Render(m.suite[m.cursor].Describe()) + "\n")
	if detail := m.theme.Detail(selected); detail != "" {
		b.WriteString(detail)
	}
	b.WriteString("\n" + m.theme.Verdict(m.reports) + "\n")
	b.WriteString(muted.Render("[enter] run   [a] run all   [↑↓] select   [q] quit"))
	return b.String()
}

func clamp(value, low, high int) int {
	switch {
	case value < low:
		return low
	case value > high:
		return high
	default:
		return value
	}
}

func Run(suite core.Suite, theme render.Theme, title string, opts ...tea.ProgramOption) ([]core.Report, error) {
	model := New(suite, theme, title)
	final, err := tea.NewProgram(model, opts...).Run()
	if err != nil {
		return nil, err
	}
	if finished, ok := final.(Model); ok {
		return finished.Reports(), nil
	}
	return model.Reports(), nil
}
