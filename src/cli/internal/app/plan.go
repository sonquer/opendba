package app

import (
	"context"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2/tree"

	"github.com/sonquer/tui4db/src/cli/internal/driver"
	"github.com/sonquer/tui4db/src/cli/internal/ui"
)

type explainMsg struct{ analyze bool }

type explainedMsg struct {
	plan      driver.Plan
	statement string
	analyze   bool
	err       error
}

// plan is what the server says it would do with a statement. It is an overlay
// rather than a screen because it is about the statement in the tab behind it,
// and closing it should leave that tab exactly as it was.
type plan struct {
	theme     *ui.Theme
	root      driver.PlanNode
	statement string
	total     float64
	analyzed  bool
	offset    int
	trouble   string
}

// explain asks the server what it would do. ANALYZE is a separate question
// because it does not ask: it runs the statement, and a statement that has
// already been run once is not a statement to run again by pressing a key that
// says "explain".
func (m Model) explain(msg explainMsg) (tea.Model, tea.Cmd) {
	if !m.session.Capabilities.Explain {
		return m, m.notify("this server cannot explain a statement")
	}
	statement := m.script().chosen()
	if strings.TrimSpace(statement) == "" {
		return m, m.notify("there is no statement to explain")
	}
	verdict := m.Verdict()
	if verdict.Blocked() {
		return m, m.notify(ui.Reason(verdict.Reason))
	}
	if msg.analyze && !verdict.Allowed() {
		return m, m.notify("running this statement to time it would change data")
	}
	conn := m.session.Conn
	analyze := msg.analyze
	return m, func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), readTimeout)
		defer cancel()
		found, err := conn.Explain(ctx, statement, analyze)
		return explainedMsg{plan: found, statement: statement, analyze: analyze, err: err}
	}
}

func (m Model) explained(msg explainedMsg) (tea.Model, tea.Cmd) {
	shown := &plan{
		theme:     m.theme,
		root:      msg.plan.Root,
		statement: msg.statement,
		total:     msg.plan.Total,
		analyzed:  msg.analyze,
	}
	if msg.err != nil {
		shown.trouble = msg.err.Error()
	}
	m.plan = shown
	return m, nil
}

// confirmAnalyze asks before timing a statement, because timing it means
// running it.
func (m Model) confirmAnalyze() (tea.Model, tea.Cmd) {
	m.modal = ask(m.theme, "time this statement?",
		"the server cannot say how long a statement takes without running it, "+
			"so this runs it once and throws the rows away",
		explainMsg{analyze: true})
	m.modal.code = m.script().chosen()
	return m, nil
}

func (m Model) planKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.Back):
		m.plan = nil
		return m, nil
	case key.Matches(msg, m.keys.Above):
		m.plan = m.plan.scroll(-1)
		return m, nil
	case key.Matches(msg, m.keys.Below):
		m.plan = m.plan.scroll(1)
		return m, nil
	case key.Matches(msg, m.keys.Choose):
		if m.plan.analyzed {
			return m, nil
		}
		m.plan = nil
		return m.confirmAnalyze()
	}
	return m, nil
}

func (p *plan) scroll(step int) *plan {
	next := *p
	next.offset = max(next.offset+step, 0)
	return &next
}

func (p plan) view(width, height int) string {
	inner := min(ui.TextWidth(width)-6, 88)
	lines := []string{ui.SplitLine(
		p.theme.Title.Render("query plan"), p.theme.Muted.Render(p.tag()), inner), ""}
	if p.trouble != "" {
		lines = append(lines, p.theme.Error.Render("✗ "+wrap(p.trouble, inner-2)))
		return p.theme.Panel.Render(square(strings.Join(lines, "\n"), inner))
	}
	body, more := ui.Window(p.body(inner), p.offset, max(height-12, 4))
	lines = append(lines, body)
	if more > 0 {
		lines = append(lines, p.theme.Subtle.Render("  ↓ "+ui.Plural(more, "more", "more")))
	}
	lines = append(lines, "", p.theme.Hints(p.hints()...))
	return p.theme.Panel.Render(square(strings.Join(lines, "\n"), inner))
}

func (p plan) tag() string {
	if p.analyzed {
		return "as it ran"
	}
	return "as the server would run it"
}

func (p plan) hints() []ui.Hint {
	hints := []ui.Hint{{Key: "↑↓", Does: "scroll"}, {Key: "esc", Does: "close"}}
	if !p.analyzed {
		hints = append([]ui.Hint{{Key: "enter", Does: "time it"}}, hints...)
	}
	return hints
}

// body draws the plan as the tree it is, with what each step costs beside it.
// A server that reports no cost gets no bar rather than a bar of nought, which
// would read as a step that costs nothing.
func (p plan) body(width int) string {
	drawn := p.branch()
	for _, child := range p.root.Children {
		p.graft(drawn, child, width)
	}
	head := p.theme.Value.Render(p.root.Name)
	if len(p.root.Children) == 0 {
		return head
	}
	return head + "\n" + drawn.String()
}

func (p plan) graft(onto *tree.Tree, node driver.PlanNode, width int) {
	branch := p.branch().Root(p.label(node, width))
	for _, child := range node.Children {
		p.graft(branch, child, width)
	}
	onto.Child(branch)
}

func (p plan) branch() *tree.Tree {
	return tree.New().
		Enumerator(tree.DefaultEnumerator).
		EnumeratorStyle(p.theme.Divider).
		IndenterStyle(p.theme.Divider)
}

// label is one step of the plan: what it does, and what it costs said in the
// only terms the server gave.
func (p plan) label(node driver.PlanNode, width int) string {
	name := p.theme.Value.Render(node.Name)
	if detail := trimmed4Plan(node); detail != "" {
		name += " " + p.theme.Muted.Render(detail)
	}
	said := p.cost(node)
	if said == "" {
		return ui.Truncate(name, width-8)
	}
	return ui.SplitLine(ui.Truncate(name, width-24), p.theme.Subtle.Render(said), width-6)
}

// trimmed4Plan is what the detail adds to the name. A driver whose detail
// starts with the name it already gave has nothing to add, and saying it twice
// is how a plan ends up reading "SCAN SCAN users".
func trimmed4Plan(node driver.PlanNode) string {
	detail := strings.TrimSpace(node.Detail)
	if node.Name == "" || !strings.HasPrefix(detail, node.Name) {
		return detail
	}
	return strings.TrimSpace(strings.TrimPrefix(detail, node.Name))
}

func (p plan) cost(node driver.PlanNode) string {
	parts := make([]string, 0, 3)
	if node.Rows > 0 {
		parts = append(parts, ui.Plural(int(node.Rows), "row", "rows"))
	}
	if node.Duration > 0 {
		parts = append(parts, driver.Duration(node.Duration))
	}
	if node.Cost > 0 && p.total > 0 {
		parts = append(parts, p.share(node))
	}
	return ui.Dotted(parts...)
}

// share is what proportion of the whole plan one step is, which is the number
// that says where the time is going.
func (p plan) share(node driver.PlanNode) string {
	ratio := node.Cost / p.total
	if ratio > 1 {
		ratio = 1
	}
	return p.theme.Track(ratio, planBarWidth)
}

// planBarWidth is how wide the share of a step is drawn.
const planBarWidth = 8
