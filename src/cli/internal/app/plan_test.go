package app

import (
	"strings"
	"testing"

	"github.com/sonquer/tui4db/src/cli/internal/driver"
	"github.com/sonquer/tui4db/src/cli/internal/ui"
)

func explaining(t *testing.T, conn *fakeConn) Model {
	t.Helper()
	m := loadedWith(t, conn, workspaceWith(t))
	m.width, m.height = 110, 36
	m.session.Capabilities.Explain = true
	editing, _ := press(t, m, "e")
	return typeInto(t, editing, "SELECT * FROM users")
}

// The server is asked what it would do, and what it says is drawn as the tree
// it is.
func TestTheServerSaysWhatItWouldDo(t *testing.T) {
	m := explaining(t, healthy())
	asked, cmd := press(t, m, "f6")
	if cmd == nil {
		t.Fatal("f6 must ask the server")
	}
	shown := settle(t, asked, cmd)
	if shown.plan == nil {
		t.Fatal("and what it says must be drawn")
	}
	view := plain(shown.content())
	for _, want := range []string{"query plan", "Limit", "Seq Scan", "on users", "2 rows"} {
		if !strings.Contains(view, want) {
			t.Errorf("the plan must show %q:\n%s", want, view)
		}
	}
	if !strings.Contains(view, "as the server would run it") {
		t.Errorf("and say it has not been run:\n%s", view)
	}
	closed, _ := press(t, shown, "esc")
	if closed.plan != nil {
		t.Error("esc must close it")
	}
	if closed.statement() != "SELECT * FROM users" {
		t.Error("and leave the tab as it was")
	}
}

// Timing a statement means running it, so it is asked about.
func TestTimingAStatementIsAskedAbout(t *testing.T) {
	m := explaining(t, healthy())
	asked, cmd := press(t, m, "f6")
	shown := settle(t, asked, cmd)

	timing, _ := press(t, shown, "enter")
	if timing.plan != nil || timing.modal == nil {
		t.Fatal("enter must ask before it runs the statement")
	}
	said := plain(timing.content())
	if !strings.Contains(said, "without running it") {
		t.Errorf("and say why:\n%s", said)
	}
	answered, cmd := press(t, timing, "enter")
	timed := pump(t, answered, cmd)
	if timed.plan == nil {
		t.Fatal("saying yes must time it")
	}
	view := plain(timed.content())
	if !strings.Contains(view, "as it ran") {
		t.Errorf("a timed plan says it ran:\n%s", view)
	}
	if !strings.Contains(view, "3ms") {
		t.Errorf("and how long each step took:\n%s", view)
	}
	if _, cmd := timed.planKey(keyMsg("enter")); cmd != nil {
		t.Error("and there is nothing left to time")
	}
}

// A server that cannot explain is not asked, and a statement that is not there
// is not explained.
func TestNothingIsExplainedWhenThereIsNothingToExplain(t *testing.T) {
	for _, want := range []struct {
		name  string
		build func(*testing.T) Model
		said  string
	}{
		{
			name: "the server cannot",
			build: func(t *testing.T) Model {
				m := explaining(t, healthy())
				m.session.Capabilities.Explain = false
				return m
			},
			said: "cannot explain",
		},
		{
			name: "there is no statement",
			build: func(t *testing.T) Model {
				m := loadedWith(t, healthy(), workspaceWith(t))
				m.session.Capabilities.Explain = true
				editing, _ := press(t, m, "e")
				return editing
			},
			said: "no statement to explain",
		},
		{
			name: "the guard refuses it",
			build: func(t *testing.T) Model {
				m := loadedWith(t, healthy(), workspaceWith(t))
				m.session.Capabilities.Explain = true
				editing, _ := press(t, m, "e")
				return typeInto(t, editing, "DROP TABLE users")
			},
			said: "READ ONLY",
		},
	} {
		t.Run(want.name, func(t *testing.T) {
			m := want.build(t)
			refused, cmd := m.explain(explainMsg{})
			if cmd == nil {
				t.Fatal("it has to say why not")
			}
			if said := refused.(Model).text(); !strings.Contains(said, want.said) {
				t.Errorf("text = %q, want something about %q", said, want.said)
			}
		})
	}
}

// Timing a statement that changes data is refused before it is asked about.
func TestTimingAWriteIsRefused(t *testing.T) {
	m := NewModel(writable(healthy()), workspaceWith(t))
	m = settle(t, m, m.load())
	m.width, m.height = 110, 36
	m.session.Capabilities.Explain = true
	editing, _ := press(t, m, "e")
	typed := typeInto(t, editing, "DELETE FROM users")
	refused, cmd := typed.explain(explainMsg{analyze: true})
	if cmd == nil {
		t.Fatal("it has to say why not")
	}
	if said := refused.(Model).text(); !strings.Contains(said, "would change data") {
		t.Errorf("text = %q", said)
	}
}

// A server that fails to explain says so rather than drawing an empty plan.
func TestAPlanThatCouldNotBeReadSaysWhy(t *testing.T) {
	conn := healthy()
	conn.failOn = "explain"
	m := explaining(t, conn)
	asked, cmd := press(t, m, "f6")
	shown := settle(t, asked, cmd)
	if shown.plan == nil {
		t.Fatal("something must be drawn")
	}
	if !strings.Contains(plain(shown.content()), "explain failed") {
		t.Errorf("it must say what went wrong:\n%s", plain(shown.content()))
	}
}

// A driver that reports no cost gets no bar, and a detail that repeats the name
// is not said twice.
func TestAPlanSaysOnlyWhatTheServerSaid(t *testing.T) {
	drawn := plan{theme: ui.Default(), root: driver.PlanNode{Name: "query plan"}}
	if got := drawn.cost(driver.PlanNode{Name: "SCAN"}); got != "" {
		t.Errorf("cost = %q, a server that measured nothing reports nothing", got)
	}
	if got := drawn.body(60); !strings.Contains(got, "query plan") {
		t.Errorf("body = %q", got)
	}
	for _, want := range []struct {
		name   string
		node   driver.PlanNode
		detail string
	}{
		{"nothing to add", driver.PlanNode{Name: "SCAN", Detail: "SCAN users"}, "users"},
		{"something to add", driver.PlanNode{Name: "Seq Scan", Detail: "on users"}, "on users"},
		{"nothing at all", driver.PlanNode{Name: "SCAN"}, ""},
		{"no name", driver.PlanNode{Detail: "SCAN users"}, "SCAN users"},
	} {
		t.Run(want.name, func(t *testing.T) {
			if got := trimmed4Plan(want.node); got != want.detail {
				t.Errorf("detail = %q, want %q", got, want.detail)
			}
		})
	}
}

// A plan longer than the panel scrolls.
func TestALongPlanScrolls(t *testing.T) {
	deep := driver.PlanNode{Name: "query plan"}
	at := &deep
	for i := range 30 {
		at.Children = []driver.PlanNode{{Name: "step", Detail: strings.Repeat("x", i+1)}}
		at = &at.Children[0]
	}
	conn := healthy()
	conn.plan = driver.Plan{Root: deep}
	m := explaining(t, conn)
	asked, cmd := press(t, m, "f6")
	shown := settle(t, asked, cmd)
	if !strings.Contains(plain(shown.content()), "more") {
		t.Errorf("a plan taller than the panel must say there is more:\n%s",
			plain(shown.content()))
	}
	down, _ := press(t, shown, "down")
	if down.plan.offset != 1 {
		t.Errorf("offset = %d", down.plan.offset)
	}
	up, _ := press(t, down, "up")
	if up.plan.offset != 0 {
		t.Errorf("offset = %d", up.plan.offset)
	}
	held, _ := press(t, up, "up")
	if held.plan.offset != 0 {
		t.Error("the top is where it stops")
	}
}
