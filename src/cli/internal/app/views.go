package app

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/sonquer/tui4db/src/cli/internal/cli"
	"github.com/sonquer/tui4db/src/cli/internal/driver"
	"github.com/sonquer/tui4db/src/cli/internal/ui"
	"github.com/sonquer/tui4db/src/cli/pkg/sqlguard"
)

func (m Model) dashboardBody() string {
	width := ui.FrameWidth(m.width)
	sections := []string{
		m.theme.Section("health", m.counts(), width),
		"",
		m.health(width),
		"",
		m.theme.Section("schema", m.where(), width),
		"",
		m.theme.Muted.Render("  " + m.schema()),
	}
	return strings.Join(sections, "\n")
}

func (m Model) health(width int) string {
	if len(m.findings) == 0 {
		return m.theme.Muted.Render("  no health signals")
	}
	labels, values := 0, 0
	for _, finding := range m.findings {
		if width := len(finding.Subsystem); width > labels {
			labels = width
		}
		if width := len(finding.Value); width > values {
			values = width
		}
	}
	note := width - labels - values - 11
	rows := make([]string, 0, len(m.findings))
	for _, finding := range m.findings {
		rows = append(rows, m.theme.Finding(
			m.theme.Severity4Driver(finding.Severity),
			finding.Subsystem, labels,
			finding.Value, values,
			ui.Truncate(finding.Note, note),
		))
	}
	return strings.Join(rows, "\n")
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

func (m Model) queryBody() string {
	width := ui.FrameWidth(m.width)
	sections := []string{
		m.editor.View(),
		m.theme.Rule(width),
		m.verdict(width),
		m.theme.Rule(width),
		m.results.render(m.theme),
	}
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

func (m Model) helpBody() string {
	width := ui.FrameWidth(m.width)
	sections := []string{
		m.theme.Section("keys", "", width),
		"",
		m.help.FullHelpView(m.keys.FullHelp()),
		"",
		m.theme.Section("safety", "", width),
		"",
		m.theme.Muted.Render(
			"Everything you type is classified before it is sent. In READ ONLY mode a statement\n" +
				"that changes data never leaves this program."),
		"",
		m.theme.Subtle.Render(m.keys.commandNote()),
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
