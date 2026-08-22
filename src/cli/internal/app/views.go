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
	return strings.Join([]string{
		m.verdict4Health(width),
		m.theme.Muted.Render("  " + m.schema()),
		"",
		m.groups(width),
		"",
		m.sessions(width),
	}, "\n")
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
	line := m.theme.Severity(severity).Render(severity.Glyph()+" ") +
		m.theme.Value.Render(ui.Truncate(sentence, width/2))
	return ui.SplitLine(line, m.counts(), width)
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
	for _, finding := range m.findings {
		if group != every && finding.Group != group {
			continue
		}
		readings = append(readings, ui.Reading{
			Severity: m.theme.Severity4Driver(finding.Severity),
			Label:    finding.Subsystem,
			Value:    finding.Value,
			Note:     finding.Note,
			Ratio:    finding.Ratio,
			Measured: finding.Measured,
		})
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

func (m Model) counts() string {
	healthy, warnings, failing := 0, 0, 0
	for _, finding := range m.findings {
		switch finding.Severity {
		case driver.SeverityCritical:
			failing++
		case driver.SeverityWarn:
			warnings++
		default:
			healthy++
		}
	}
	return m.theme.Counts(healthy, warnings, failing)
}

func (m Model) schema() string {
	return ui.Dotted(
		m.where(),
		ui.Plural(len(m.tables), "table", "tables"),
		ui.Plural(len(m.indexes), "index", "indexes"),
		ui.ByteSize(m.totalSize()),
	)
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

// paneWidth is how wide the editor and its results are, which depends on
// whether the schema is beside them.
func (m Model) paneWidth() int {
	width := ui.FrameWidth(m.width)
	if m.zoomed || m.sidebar.hidden {
		return width
	}
	return width - m.sidebar.width(width) - 3
}

// editorPane stacks the statement, how it was classified, and what it returned.
func (m Model) editorPane(width, height int) string {
	rows := editorRows(m.height)
	sections := []string{
		lipgloss.NewStyle().Height(rows).MaxHeight(rows).Render(m.editor.View()),
		m.theme.Rule(width),
		m.verdict(width),
		m.theme.Rule(width),
		m.results.render(m.theme),
	}
	return ui.Fit(strings.Join(sections, "\n"), height)
}

// zoomBody gives the result the whole window, with the statement that produced
// it above, rendered the way a code block in a document would be.
func (m Model) zoomBody() string {
	width := ui.FrameWidth(m.width)
	sections := []string{}
	if statement := m.theme.Markdown(width).SQL(m.results.statement); statement != "" {
		sections = append(sections, statement, m.theme.Rule(width))
	}
	sections = append(sections, m.results.render(m.theme))
	return strings.Join(sections, "\n")
}

// schemaBody names the schema a list came from, so an empty one reads as a
// place to leave rather than a database with nothing in it.
func (m Model) schemaBody(title, list string) string {
	return strings.Join([]string{
		m.theme.Section(title, m.where(), ui.FrameWidth(m.width)),
		"",
		list,
	}, "\n")
}

func (m Model) where() string {
	if schema := m.session.Connection.Schema(); schema != "" {
		return schema
	}
	return "every schema"
}

// verdict says how the statement would be classified, without shouting at
// someone who is still typing it.
func (m Model) verdict(width int) string {
	if strings.TrimSpace(m.statement()) == "" {
		return m.theme.Muted.Render("nothing to run yet")
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
