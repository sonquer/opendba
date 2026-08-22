package app

import (
	_ "embed"
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"

	tea "charm.land/bubbletea/v2"

	"github.com/sonquer/tui4db/src/cli/internal/cli"
	"github.com/sonquer/tui4db/src/cli/internal/driver"
	"github.com/sonquer/tui4db/src/cli/internal/ui"
	"github.com/sonquer/tui4db/src/cli/pkg/sqlguard"
)

func (m Model) dashboardBody() string {
	width := ui.FrameWidth(m.width)
	sections := []string{
		m.verdict4Health(width),
		"",
		m.groups(width),
	}
	if running := m.sessions(width); running != "" {
		sections = append(sections, "", running)
	}
	return strings.Join(sections, "\n")
}

// sessions is the list of what the server is running, for the drivers that
// have more than one session to speak of.
func (m Model) sessions(width int) string {
	if !m.session.Capabilities.Sessions {
		return ""
	}
	return m.running.view(width, m.onSessions)
}

// verdict4Health is the line someone who does not run databases for a living
// reads and stops: what needs attention, named.
func (m Model) verdict4Health(width int) string {
	worst, trouble := driver.SeverityOK, []string{}
	for _, finding := range m.findings {
		switch finding.Severity {
		case driver.SeverityCritical:
			worst = driver.SeverityCritical
			trouble = append(trouble, finding.Subsystem)
		case driver.SeverityWarn:
			if worst != driver.SeverityCritical {
				worst = driver.SeverityWarn
			}
			trouble = append(trouble, finding.Subsystem)
		}
	}
	severity := m.theme.Severity4Driver(worst)
	sentence := "nothing needs attention"
	if len(trouble) > 0 {
		sentence = ui.Plural(len(trouble), "thing needs", "things need") +
			" attention: " + strings.Join(trouble, ", ")
	}
	return m.theme.Severity(severity).Render(severity.Glyph()+" ") +
		m.theme.Value.Render(ui.Clip(sentence, width-2))
}

// twoColumns is where a second column still leaves room for the plain words
// beside each reading. Below it, one column keeps the sentences readable.
const twoColumns = 140

// groups lays the readings out the way the findings are grouped, in two columns
// when there is room for two.
func (m Model) groups(width int) string {
	column := width
	if width >= twoColumns {
		column = width/2 - 2
	}
	at := ui.Measure(m.readings(every))
	blocks := make([]string, 0, 5)
	for _, group := range []string{
		driver.GroupMemory, driver.GroupLoad, driver.GroupScans, driver.GroupStorage, "",
	} {
		if block := m.group(group, column, at); block != "" {
			blocks = append(blocks, block)
		}
	}
	if len(blocks) == 0 {
		return m.theme.Muted.Render("  no health signals")
	}
	if width < twoColumns {
		return strings.Join(blocks, "\n\n")
	}
	left, right := []string{}, []string{}
	for i, block := range blocks {
		if i%2 == 0 {
			left = append(left, block)
			continue
		}
		right = append(right, block)
	}
	half := width / 2
	return lipgloss.JoinHorizontal(lipgloss.Top,
		lipgloss.NewStyle().Width(half).Render(strings.Join(left, "\n\n")),
		lipgloss.NewStyle().Width(width-half).Render(strings.Join(right, "\n\n")))
}

// readings turns findings into rows, for one group or, with an empty name that
// no finding uses, for measuring them all at once.
func (m Model) readings(group string) []ui.Reading {
	readings := make([]ui.Reading, 0, len(m.findings))
	at := 0
	for _, finding := range m.findings {
		reading := ui.Reading{
			Code:     finding.Code,
			Severity: m.theme.Severity4Driver(finding.Severity),
			Label:    finding.Subsystem,
			Value:    finding.Value,
			Note:     finding.Note,
			Ratio:    finding.Ratio,
			Measured: finding.Measured,
			Cursor:   at == m.reading && !m.onSessions,
		}
		at++
		if group != every && finding.Group != group {
			continue
		}
		readings = append(readings, reading)
	}
	return readings
}

// every is a group name no finding carries, which is how one call measures all
// of them at once.
const every = "*"

func (m Model) group(name string, width int, at ui.Columns) string {
	readings := m.readings(name)
	if len(readings) == 0 {
		return ""
	}
	title := name
	if title == "" {
		title = "health"
	}
	return m.theme.Section(title, "", width) + "\n\n" + m.theme.Readings(readings, width, at)
}

func (m Model) totalSize() int64 {
	var total int64
	for _, table := range m.tables {
		if table.Size > 0 {
			total += table.Size
		}
	}
	return total
}

func (m Model) paneWidth() int {
	width := ui.FrameWidth(m.width)
	if m.zoomed || m.sidebar.hidden {
		return width
	}
	return width - m.sidebar.width(width) - 3
}

// editorPane stacks the statement, how it was classified, and what it returned.
func (m Model) editorPane(width, height int) string {
	rows := m.editorRows()
	sections := []string{
		lipgloss.NewStyle().Height(rows).MaxHeight(rows).Render(m.statementView(width)),
		m.theme.Rule(width),
	}
	if verdict := m.verdict(width); verdict != "" {
		sections = append(sections, verdict, m.theme.Rule(width))
	}
	sections = append(sections, m.results.render(m.theme, m.focus == focusResults))
	return ui.Fit(strings.Join(sections, "\n"), height)
}

// statementView keeps the editor plain while it is being typed into, because a
// textarea has no way to colour what is inside it, and shows the statement
// highlighted the moment the cursor is somewhere else.
func (m Model) statementView(width int) string {
	if m.focus == focusEditor || strings.TrimSpace(m.statement()) == "" {
		return m.editor.View()
	}
	return m.theme.Markdown(width).SQL(m.statement())
}

// zoomBody gives the result the whole window, with the statement that produced
// it above, rendered the way a code block in a document would be.
func (m Model) zoomBody() string {
	width := ui.FrameWidth(m.width)
	sections := []string{}
	if statement := m.theme.Markdown(width).SQL(m.results.statement); statement != "" {
		sections = append(sections, statement, m.theme.Rule(width))
	}
	sections = append(sections, m.results.render(m.theme, true))
	return strings.Join(sections, "\n")
}

// schemaBody names the schema a list came from, so an empty one reads as a
// place to leave rather than a database with nothing in it.
func (m Model) schemaBody(title, list string) string {
	tag := ui.Dotted(m.where(),
		ui.Plural(len(m.tables), "table", "tables"),
		ui.ByteSize(m.totalSize()))
	return strings.Join([]string{
		m.theme.Section(title, m.theme.Muted.Render(tag), ui.FrameWidth(m.width)),
		"",
		list,
	}, "\n")
}

// where names the schemas a listing came from, which is the form's answer and
// not the default schema for unqualified names.
func (m Model) where() string {
	filter := m.session.Connection.Filter()
	if len(filter) == 0 {
		return "every schema"
	}
	return strings.Join(filter, ", ")
}

// verdict says how the statement would be classified, without shouting at
// someone who is still typing it. An empty editor has nothing to classify, and
// the empty result underneath already says there is nothing here yet.
func (m Model) verdict(width int) string {
	if strings.TrimSpace(m.statement()) == "" {
		return ""
	}
	result := m.Verdict()
	if unfinished(result) {
		return m.theme.Subtle.Render("… the statement is not finished")
	}
	return m.theme.Verdict(result, width)
}

func unfinished(result sqlguard.Result) bool {
	return result.Verdict == sqlguard.Block && strings.Contains(result.Reason, "'<EOF>'")
}

//go:embed help.md
var helpDocument string

// helpBody is a document, rendered by Glamour, with the keys generated from the
// keymap so the page and the program cannot drift apart.
func (m Model) helpBody() string {
	width := ui.FrameWidth(m.width)
	sections := []string{
		m.theme.Section("keys", "", width),
		"",
		m.help.FullHelpView(m.keys.FullHelp()),
		"",
		m.theme.Subtle.Render(m.keys.commandNote()),
		"",
		m.theme.Markdown(width).Render(helpDocument),
	}
	return strings.Join(sections, "\n")
}

func Launch(session cli.Session, workspace cli.Workspace) error {
	return launch(session, workspace)
}

func launch(session cli.Session, workspace cli.Workspace, options ...tea.ProgramOption) error {
	program := tea.NewProgram(NewModel(session, workspace), options...)
	final, err := program.Run()
	if model, ok := final.(Model); ok {
		model.release()
	}
	if err != nil {
		return fmt.Errorf("run the interface: %w", err)
	}
	return nil
}
