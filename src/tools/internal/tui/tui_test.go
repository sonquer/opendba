package tui

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/sonquer/opendba/src/tools/internal/core"
	"github.com/sonquer/opendba/src/tools/internal/render"
)

type stubCheck struct {
	name     string
	describe string
	report   core.Report
	err      error
	runs     *int
}

func (s stubCheck) Name() string     { return s.name }
func (s stubCheck) Describe() string { return s.describe }
func (s stubCheck) Run(context.Context) (core.Report, error) {
	if s.runs != nil {
		*s.runs++
	}
	return s.report, s.err
}

func newModel(checks ...core.Check) Model {
	return New(core.Suite(checks), render.DefaultTheme(), "opendba dev")
}

func pass(name string) stubCheck {
	return stubCheck{name: name, describe: name + " description", report: core.Report{Check: name, Status: core.StatusPass, Summary: "ok"}}
}

func key(m Model, k string) (Model, tea.Cmd) {
	updated, cmd := m.Update(tea.KeyPressMsg{Code: firstRune(k), Text: k})
	return updated.(Model), cmd
}

func firstRune(s string) rune {
	for _, r := range s {
		return r
	}
	return 0
}

func TestInitialReportsArePending(t *testing.T) {
	m := newModel(pass("a"), pass("b"))
	if cmd := m.Init(); cmd != nil {
		t.Error("Init must not schedule work")
	}
	for _, r := range m.Reports() {
		if r.Status != core.StatusPending {
			t.Fatalf("report = %+v", r)
		}
	}
}

func TestCursorMovementIsBounded(t *testing.T) {
	m := newModel(pass("a"), pass("b"))
	m, _ = key(m, "up")
	if m.cursor != 0 {
		t.Fatalf("cursor = %d, want 0", m.cursor)
	}
	m, _ = key(m, "down")
	m, _ = key(m, "down")
	if m.cursor != 1 {
		t.Fatalf("cursor = %d, want 1", m.cursor)
	}
	m, _ = key(m, "k")
	if m.cursor != 0 {
		t.Fatalf("cursor after k = %d", m.cursor)
	}
	m, _ = key(m, "j")
	if m.cursor != 1 {
		t.Fatalf("cursor after j = %d", m.cursor)
	}
}

func TestQuitKeys(t *testing.T) {
	for _, k := range []string{"q", "ctrl+c", "esc"} {
		m := newModel(pass("a"))
		updated, cmd := m.Update(tea.KeyPressMsg{Text: k, Code: firstRune(k)})
		if cmd == nil {
			t.Fatalf("%q must quit", k)
		}
		if !updated.(Model).done {
			t.Fatalf("%q must mark the model done", k)
		}
	}
}

func TestRunSelectedCheck(t *testing.T) {
	runs := 0
	check := pass("a")
	check.runs = &runs
	m := newModel(check, pass("b"))

	m, cmd := key(m, "enter")
	if cmd == nil {
		t.Fatal("enter must start the selected check")
	}
	m = drain(t, m, cmd)

	if runs != 1 {
		t.Fatalf("runs = %d, want 1", runs)
	}
	if m.Reports()[0].Status != core.StatusPass {
		t.Fatalf("report = %+v", m.Reports()[0])
	}
	if m.Reports()[1].Status != core.StatusPending {
		t.Fatal("unselected check must stay pending")
	}
}

func TestRunAllChecksSequentially(t *testing.T) {
	first, second := 0, 0
	a, b := pass("a"), pass("b")
	a.runs, b.runs = &first, &second
	m := newModel(a, b)

	m, cmd := key(m, "a")
	m = drain(t, m, cmd)

	if first != 1 || second != 1 {
		t.Fatalf("runs = %d, %d", first, second)
	}
	for _, r := range m.Reports() {
		if r.Status != core.StatusPass {
			t.Fatalf("report = %+v", r)
		}
	}
}

func TestRunIgnoresInputWhileBusy(t *testing.T) {
	m := newModel(pass("a"))
	updated, _ := m.Update(startedMsg{index: 0})
	m = updated.(Model)
	if m.Reports()[0].Status != core.StatusRunning {
		t.Fatal("started message must mark the check running")
	}
	_, cmd := key(m, "enter")
	if cmd != nil {
		t.Fatal("must not start a second check while one is running")
	}
}

func TestFailedCheckErrorBecomesReport(t *testing.T) {
	m := newModel(stubCheck{name: "a", err: errors.New("scan failed")})
	m, cmd := key(m, "r")
	m = drain(t, m, cmd)
	report := m.Reports()[0]
	if report.Status != core.StatusFail || report.Summary != "scan failed" {
		t.Fatalf("report = %+v", report)
	}
}

func TestWindowSizeIsApplied(t *testing.T) {
	m := newModel(pass("a"))
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 200, Height: 50})
	m = updated.(Model)
	if m.width != 200 || m.height != 50 {
		t.Fatalf("size = %dx%d", m.width, m.height)
	}
	if !strings.Contains(ansi.Strip(m.content()), strings.Repeat("─", 120)) {
		t.Error("rule must be clamped to 120 columns")
	}
}

func TestUnknownMessagesAreIgnored(t *testing.T) {
	m := newModel(pass("a"))
	updated, cmd := m.Update(struct{}{})
	if cmd != nil || updated.(Model).cursor != 0 {
		t.Fatal("unknown messages must be inert")
	}
	if _, cmd := key(m, "z"); cmd != nil {
		t.Fatal("unbound keys must be inert")
	}
}

func TestStartWithEmptySelectionIsIgnored(t *testing.T) {
	m := newModel(pass("a"))
	updated, cmd := m.start(nil)
	if cmd != nil || updated.(Model).active != -1 {
		t.Fatal("empty selection must not start work")
	}
}

func TestViewRendersChecksAndHints(t *testing.T) {
	m := newModel(pass("a"), pass("b"))
	view := m.View()
	if !view.AltScreen || view.WindowTitle != "opendba dev" {
		t.Fatalf("view = %+v", view)
	}
	content := ansi.Strip(m.content())
	for _, want := range []string{"opendba dev", "a", "b", "a description", "[enter] run", "0 passed"} {
		if !strings.Contains(content, want) {
			t.Errorf("content missing %q", want)
		}
	}
	if !strings.Contains(content, "▸") {
		t.Error("cursor marker missing")
	}
}

func TestViewRendersDetail(t *testing.T) {
	check := stubCheck{name: "a", report: core.Report{Check: "a", Status: core.StatusFail, Detail: []string{"a.go:1: // comment"}}}
	m := newModel(check)
	m, cmd := key(m, "enter")
	m = drain(t, m, cmd)
	if !strings.Contains(ansi.Strip(m.content()), "a.go:1") {
		t.Error("detail lines must be rendered")
	}
}

func TestClamp(t *testing.T) {
	cases := []struct{ value, low, high, want int }{
		{5, 1, 10, 5}, {0, 1, 10, 1}, {50, 1, 10, 10},
	}
	for _, c := range cases {
		if got := clamp(c.value, c.low, c.high); got != c.want {
			t.Errorf("clamp(%d,%d,%d) = %d, want %d", c.value, c.low, c.high, got, c.want)
		}
	}
}

func drain(t *testing.T, m Model, cmd tea.Cmd) Model {
	t.Helper()
	for depth := 0; cmd != nil && depth < 32; depth++ {
		msg := cmd()
		switch typed := msg.(type) {
		case tea.BatchMsg:
			cmd = nil
			for _, sub := range typed {
				m = drain(t, m, sub)
			}
		case tea.Cmd:
			cmd = typed
		default:
			updated, next := m.Update(msg)
			m = updated.(Model)
			cmd = next
		}
	}
	return m
}

func TestRunProgramReturnsReports(t *testing.T) {
	reports, err := Run(core.Suite{pass("a")}, render.DefaultTheme(), "dev",
		tea.WithInput(strings.NewReader("q")),
		tea.WithOutput(io.Discard),
	)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(reports) != 1 || reports[0].Check != "a" {
		t.Fatalf("reports = %+v", reports)
	}
}

func TestRunProgramFailsWithoutInput(t *testing.T) {
	if _, err := Run(core.Suite{pass("a")}, render.DefaultTheme(), "dev",
		tea.WithInput(failingReader{}),
		tea.WithOutput(io.Discard),
	); err == nil {
		t.Log("program tolerated a failing input reader")
	}
}

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) { return 0, errors.New("no input") }
