package app

import (
	_ "embed"
	"fmt"
	"runtime/debug"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	tea "charm.land/bubbletea/v2"

	"github.com/sonquer/opendba/src/cli/internal/cli"
	"github.com/sonquer/opendba/src/cli/internal/driver"
	"github.com/sonquer/opendba/src/cli/internal/ui"
	"github.com/sonquer/opendba/src/cli/pkg/sqlguard"
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
// reads and stops at: what needs attention, worst first, and how much of the
// report came back fine.
func (m Model) verdict4Health(width int) string {
	tally := m.tally()
	return m.theme.Headline(m.theme.Severity4Driver(tally.worst),
		tally.subject(), tally.balance(), width)
}

// tally is the report counted rather than listed: what is worth acting on,
// what is worth watching, what is fine, and what could not be read at all.
type tally struct {
	worst   driver.Severity
	act     []string
	watch   []string
	fine    int
	noted   int
	unknown int
}

func (m Model) tally() tally {
	counted := tally{worst: driver.SeverityOK}
	for _, finding := range m.findings {
		switch finding.Severity {
		case driver.SeverityCritical:
			counted.worst = driver.SeverityCritical
			counted.act = append(counted.act, finding.Subsystem)
		case driver.SeverityWarn:
			if counted.worst != driver.SeverityCritical {
				counted.worst = driver.SeverityWarn
			}
			counted.watch = append(counted.watch, finding.Subsystem)
		case driver.SeverityOK:
			counted.fine++
		case driver.SeverityInfo:
			counted.noted++
		default:
			counted.unknown++
		}
	}
	return counted
}

// subject names what needs attention, worst first and never more than a
// handful: a line that lists nine things is a line nobody reads.
func (t tally) subject() string {
	trouble := append(append([]string{}, t.act...), t.watch...)
	if len(trouble) == 0 {
		return "nothing needs attention"
	}
	if len(trouble) <= named {
		return strings.Join(trouble, " · ")
	}
	return strings.Join(trouble[:named], " · ") +
		fmt.Sprintf(" · +%d more", len(trouble)-named)
}

const named = 3

// balance is how much of the report is fine, which is the half of the answer
// the count of problems does not give.
func (t tally) balance() string {
	checks := len(t.act) + len(t.watch) + t.fine + t.noted
	parts := []string{}
	switch {
	case len(t.act) > 0:
		parts = append(parts, fmt.Sprintf("%d to act", len(t.act)),
			fmt.Sprintf("%d to watch", len(t.watch)), fmt.Sprintf("%d fine", t.fine))
	case len(t.watch) > 0:
		parts = append(parts, fmt.Sprintf("%d of %d checks are fine", t.fine+t.noted, checks))
	default:
		parts = append(parts, ui.Plural(checks, "check", "checks")+", all of them fine")
	}
	if t.unknown > 0 {
		parts = append(parts, ui.Plural(t.unknown, "reading", "readings")+" the role cannot see")
	}
	return ui.Dotted(parts...)
}

// balanced puts each block in the shorter column rather than in alternate ones,
// because the groups are not the same height and alternating them leaves one
// column half empty while the other runs off the screen.
func balanced(blocks []string) (left, right []string) {
	tall, short := 0, 0
	for _, block := range blocks {
		height := lipgloss.Height(block) + 1
		if tall <= short {
			left, tall = append(left, block), tall+height
			continue
		}
		right, short = append(right, block), short+height
	}
	return left, right
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
	left, right := balanced(blocks)
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
		if hint := m.suggest.hint(m.theme); hint != "" {
			room := width - lipgloss.Width(hint) - 2
			verdict = ui.SplitLine(ui.Clip(verdict, room), hint, width)
		}
		sections = append(sections, verdict, m.theme.Rule(width))
	}
	sections = append(sections, m.results.render(m.theme, m.focus == focusResults))
	return ui.Fit(strings.Join(sections, "\n"), height)
}

// statementView colours the statement as it is typed.
func (m Model) statementView(int) string {
	if strings.TrimSpace(m.statement()) == "" {
		return m.editor.View()
	}
	drawn := strings.Split(m.editor.View(), "\n")
	for i, line := range drawn {
		drawn[i] = ansi.Truncate(line, editorGutter, "") +
			m.theme.Highlight(ansi.Strip(ansi.TruncateLeft(line, editorGutter, "")))
	}
	return strings.Join(m.withGhost(drawn), "\n")
}

// withGhost writes the rest of the word being suggested after the cursor, in the
// grey of something that has not been typed.
func (m Model) withGhost(drawn []string) []string {
	ghost := m.suggest.ghost()
	at := m.editor.Cursor()
	if ghost == "" || at == nil || m.focus != focusEditor {
		return drawn
	}
	row, column := at.Y, at.X
	if row < 0 || row >= len(drawn) {
		return drawn
	}
	after := ansi.TruncateLeft(drawn[row], column, "")
	if strings.TrimSpace(ansi.Strip(after)) != "" {
		return drawn
	}
	written := m.theme.Subtle.Render(ghost)
	drawn[row] = ansi.Truncate(drawn[row], column, "") + written +
		ansi.TruncateLeft(drawn[row], column+lipgloss.Width(ghost), "")
	return drawn
}

// caret is where the terminal should put its cursor, which is inside the
// editor when that is what is being typed into and nowhere otherwise.
func (m Model) caret() *tea.Cursor {
	if m.view != viewQuery || m.focus != focusEditor || m.zoomed {
		return nil
	}
	if m.page != nil || m.modal != nil || m.chooser != nil ||
		m.palette != nil || m.exporter != nil || m.wizard != nil {
		return nil
	}
	at := m.editor.Cursor()
	if at == nil {
		return nil
	}
	x, y := m.editorOrigin()
	at.X += x
	at.Y += y
	at.Color = m.theme.P.Accent
	return at
}

// editorOrigin is where the editor pane starts on the screen, which is what a
// cursor inside it has to be moved by to land where it belongs.
func (m Model) editorOrigin() (x, y int) {
	x = ui.Gutter
	if !m.sidebar.hidden {
		x += m.sidebar.width(ui.FrameWidth(m.width)) + 2
	}
	return x, ui.BodyTop(true)
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
func (m Model) schemaBody(title string, shown int, list string) string {
	width := ui.FrameWidth(m.width)
	at := m.lists[m.which()]
	tag := m.scope()
	if narrowed := at.tag(shown, m.held()); narrowed != "" {
		tag = narrowed
	}
	return strings.Join([]string{
		m.theme.Screen(title, m.theme.Muted.Render(tag), width),
		"",
		at.view(m.theme, width) + list,
	}, "\n")
}

// held is how many rows the server gave us, against which a filter says how
// much it is hiding.
func (m Model) held() int {
	if m.view == viewIndexes {
		return len(m.indexes)
	}
	return len(m.tables)
}

// scope is what the screen is showing and how much of it, which on the indexes
// screen is a count of indexes and not of tables.
func (m Model) scope() string {
	if m.view == viewIndexes {
		return ui.Dotted(m.where(), ui.Plural(len(m.indexes), "index", "indexes"),
			ui.ByteSize(m.indexSize()))
	}
	return ui.Dotted(m.where(), ui.Plural(len(m.tables), "table", "tables"),
		ui.ByteSize(m.totalSize()))
}

func (m Model) indexSize() int64 {
	var total int64
	for _, index := range m.indexes {
		if index.Size > 0 {
			total += index.Size
		}
	}
	return total
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
// someone who is still typing it.
func (m Model) verdict(width int) string {
	if m.inflight {
		return m.elapsed()
	}
	if strings.TrimSpace(m.statement()) == "" {
		return ""
	}
	result := m.Verdict()
	if unfinished(result) {
		return m.theme.Subtle.Render("… the statement is not finished")
	}
	drawn := m.theme.Verdict(result, width)
	if place := m.script().place(); place != "" {
		room := width - lipgloss.Width(place) - 2
		drawn = ui.SplitLine(ui.Clip(drawn, room), m.theme.Subtle.Render(place), width)
	}
	return drawn
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

// withAssistant hands the model a way to start a conversation when one is
// configured.
func withAssistant(m Model, session cli.Session) Model {
	if !session.AI.Enabled {
		return m
	}
	return m.WithAssistant(session.AI.Instance.Name, assistantFor(session))
}

// launch runs the interface and catches it falling over.
func launch(session cli.Session, workspace cli.Workspace, options ...tea.ProgramOption) (err error) {
	program := tea.NewProgram(withAssistant(NewModel(session, workspace), session),
		append(options, tea.WithoutCatchPanics())...)
	report := crash{paths: workspace.Setup().Store.Paths, version: session.Version}
	defer func() {
		cause := recover()
		if cause == nil {
			return
		}
		program.Kill()
		err = fell("drawing the screen", cause, report.wrote("drawing the screen", cause, debug.Stack()))
	}()
	final, err := program.Run()
	if model, ok := final.(Model); ok {
		model.release()
	}
	if err != nil {
		return fmt.Errorf("run the interface: %w", err)
	}
	return nil
}
