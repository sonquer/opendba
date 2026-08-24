package app

import (
	"strings"
	"testing"

	"github.com/sonquer/opendba/src/cli/internal/ui"

	tea "charm.land/bubbletea/v2"

	"github.com/sonquer/opendba/src/cli/internal/driver"
)

func withColumns(t *testing.T) Model {
	t.Helper()
	conn := healthy()
	conn.fields = map[string][]driver.Column{
		"users": {
			{Name: "id", Type: "bigint"},
			{Name: "email", Type: "text"},
			{Name: "created_at", Type: "timestamptz"},
		},
	}
	m := loadedWith(t, conn, workspaceWith(t))
	m.width, m.height = 100, 32
	editing, _ := press(t, m, "e")
	return editing
}

func TestTablesAreOffered(t *testing.T) {
	m := typeInto(t, withColumns(t), "SELECT * FROM us")
	m.width, m.height = 110, 34
	if !m.suggest.active() {
		t.Fatal("a table prefix must be offered")
	}
	if ghost := m.suggest.ghost(); ghost != "ers" {
		t.Errorf("ghost = %q, what is drawn after the cursor is the rest of the word", ghost)
	}
	view := plain(m.content())
	if !strings.Contains(view, "SELECT * FROM users") {
		t.Errorf("the rest of the word must be drawn where it would be typed:\n%s", view)
	}
	if !strings.Contains(view, "tab to accept") {
		t.Errorf("and the screen must say how to take it:\n%s", view)
	}
	if !strings.Contains(plain(m.footer(0)), "accept") {
		t.Errorf("footer = %s", plain(m.footer(0)))
	}
}

// A suggestion that does not carry on from what was typed is not drawn after
// the cursor, because tab would not leave that text behind.
func TestOnlySomethingThatCarriesOnIsDrawn(t *testing.T) {
	m := typeInto(t, withColumns(t), "SELECT * FROM us")
	elsewhere := m
	elsewhere.suggest.items = []suggestion{{text: "orders", kind: "table"}}
	if ghost := elsewhere.suggest.ghost(); ghost != "" {
		t.Errorf("ghost = %q", ghost)
	}
	none := m
	none.suggest.items = nil
	if ghost := none.suggest.ghost(); ghost != "" {
		t.Errorf("ghost = %q, nothing is being suggested", ghost)
	}
	if hint := none.suggest.hint(none.theme); hint != "" {
		t.Errorf("hint = %q", hint)
	}
}

// Text already written is never covered by a suggestion.
func TestASuggestionNeverCoversWhatIsWritten(t *testing.T) {
	m := typeInto(t, withColumns(t), "SELECT * FROM us WHERE 1")
	for range 8 {
		m, _ = press(t, m, "left")
	}
	m, _ = m.resuggest()
	if !m.suggest.active() {
		t.Skip("nothing is being suggested there")
	}
	drawn := m.withGhost(strings.Split(m.editor.View(), "\n"))
	if strings.Contains(plain(strings.Join(drawn, "\n")), "usersers") {
		t.Error("the suggestion was written over what was already there")
	}
}

func TestKeywordsAreOffered(t *testing.T) {
	m := typeInto(t, withColumns(t), "SEL")
	item, ok := m.suggest.selected()
	if !ok || item.text != "SELECT" || item.kind != "keyword" {
		t.Fatalf("suggestion = %+v", item)
	}
}

func TestColumnsAreOfferedForTablesInTheStatement(t *testing.T) {
	m := withColumns(t)
	m = typeInto(t, m, "SELECT * FROM users")
	loading := m.readColumns()
	if loading == nil {
		t.Fatal("the columns of a named table must be read")
	}
	filled, _ := m.Update(runFirst(t, loading))
	m = filled.(Model)
	if len(m.fields["users"]) != 3 {
		t.Fatalf("fields = %+v", m.fields)
	}
	if m.readColumns() != nil {
		t.Error("columns are read once")
	}

	m = typeInto(t, m, " WHERE ema")
	item, ok := m.suggest.selected()
	if !ok || item.text != "email" || item.kind != "text" {
		t.Fatalf("suggestion = %+v", item)
	}
}

func TestQualifiedColumnsAreOffered(t *testing.T) {
	m := withColumns(t)
	m.fields["users"] = []driver.Column{{Name: "email", Type: "text"}}
	m = typeInto(t, m, "SELECT users.em")
	item, ok := m.suggest.selected()
	if !ok || item.text != "users.email" {
		t.Fatalf("suggestion = %+v", item)
	}
	accepted, _ := press(t, m, "tab")
	if accepted.statement() != "SELECT users.email" {
		t.Errorf("statement = %q", accepted.statement())
	}
}

func TestAcceptingASuggestion(t *testing.T) {
	m := typeInto(t, withColumns(t), "SELECT * FROM us")
	accepted, _ := press(t, m, "tab")
	if accepted.statement() != "SELECT * FROM users" {
		t.Errorf("statement = %q", accepted.statement())
	}
	if accepted.suggest.active() {
		t.Error("accepting closes the list")
	}
	if !strings.Contains(plain(accepted.footer(0)), "run") {
		t.Error("the footer must go back to the editor keys")
	}
}

func TestMovingThroughSuggestions(t *testing.T) {
	m := typeInto(t, withColumns(t), "SELECT * FROM ")
	m = typeInto(t, m, "u")
	if len(m.suggest.items) < 2 {
		t.Skip("this database offers a single completion")
	}
	down, _ := press(t, m, "down")
	if down.suggest.cursor != 1 {
		t.Errorf("cursor = %d", down.suggest.cursor)
	}
	up, _ := press(t, down, "up")
	if up.suggest.cursor != 0 {
		t.Errorf("cursor = %d", up.suggest.cursor)
	}
}

func TestDismissingSuggestions(t *testing.T) {
	m := typeInto(t, withColumns(t), "SELECT * FROM us")
	dismissed, _ := press(t, m, "esc")
	if dismissed.suggest.active() {
		t.Fatal("esc must close the list")
	}
	if dismissed.view != viewQuery {
		t.Error("esc must close the list before it leaves the editor")
	}
	back, _ := press(t, dismissed, "esc")
	if back.view != viewDashboard {
		t.Error("a second esc leaves the editor")
	}
}

func TestNothingIsOfferedForAnEmptyOrCompleteWord(t *testing.T) {
	m := withColumns(t)
	if m.suggest.active() {
		t.Fatal("an empty editor offers nothing")
	}
	typed := typeInto(t, m, "SELECT * FROM users")
	if typed.suggest.active() {
		t.Errorf("a finished word offers nothing: %+v", typed.suggest.items)
	}
	spaced := typeInto(t, typed, " ")
	if spaced.suggest.active() {
		t.Error("a space offers nothing")
	}
	nonsense := typeInto(t, m, "zzz")
	if nonsense.suggest.active() {
		t.Errorf("nothing matches zzz: %+v", nonsense.suggest.items)
	}
}

func TestSuggestionsSurviveAWideStatement(t *testing.T) {
	sized, _ := withColumns(t).Update(tea.WindowSizeMsg{Width: 60, Height: 14})
	m := typeInto(t, sized.(Model), strings.Repeat("SELECT * FROM users JOIN users ON 1=1 ", 2)+"us")
	if !m.suggest.active() {
		t.Fatal("a long statement still offers completions")
	}
	view := plain(m.content())
	for _, line := range strings.Split(view, "\n") {
		if len([]rune(line)) > 60 {
			t.Fatalf("the popup must stay inside the window: %q", line)
		}
	}
}

func TestColumnsThatCannotBeReadAreNotOffered(t *testing.T) {
	conn := healthy()
	conn.failOn = "columns"
	m := loadedWith(t, conn, workspaceWith(t))
	editing, _ := press(t, m, "e")
	typed := typeInto(t, editing, "SELECT * FROM users")
	answered, _ := typed.Update(runFirst(t, typed.readColumns()))
	if len(answered.(Model).fields["users"]) != 0 {
		t.Error("a failed read offers nothing")
	}
	if answered.(Model).readColumns() != nil {
		t.Error("a failed read is not retried on every keystroke")
	}
}

func catalogue(t *testing.T) Model {
	t.Helper()
	conn := healthy()
	conn.tables = []driver.Table{
		{Schema: "catalog", Name: "products", Kind: "table"},
		{Schema: "catalog", Name: "product_images", Kind: "table"},
		{Schema: "iam", Name: "permissions", Kind: "table"},
	}
	conn.fields = map[string][]driver.Column{
		"products": {{Name: "sku", Type: "text"}, {Name: "price", Type: "numeric"}},
	}
	m := loadedWith(t, conn, workspaceWith(t))
	m.width, m.height = 110, 32
	editing, _ := press(t, m, "e")
	return editing
}

func TestASchemaCompletesToItsTables(t *testing.T) {
	schemas := typeInto(t, catalogue(t), "SELECT * FROM cat")
	if item, _ := schemas.suggest.selected(); item.text != "catalog" || item.kind != "schema" {
		t.Fatalf("a prefix must offer the schemas: %+v", schemas.suggest.items)
	}

	tables := typeInto(t, catalogue(t), "SELECT * FROM catalog.")
	if len(tables.suggest.items) != 2 {
		t.Fatalf("a schema and a dot offers its tables: %+v", tables.suggest.items)
	}
	if tables.suggest.items[0].text != "catalog.products" {
		t.Errorf("suggestion = %+v", tables.suggest.items[0])
	}

	narrowed := typeInto(t, catalogue(t), "SELECT * FROM catalog.product_")
	if len(narrowed.suggest.items) != 1 || narrowed.suggest.items[0].text != "catalog.product_images" {
		t.Errorf("the stem after the dot must narrow it: %+v", narrowed.suggest.items)
	}

	accepted, _ := press(t, tables, "tab")
	if accepted.statement() != "SELECT * FROM catalog.products" {
		t.Errorf("statement = %q", accepted.statement())
	}
}

func TestAQualifiedTableCompletesItsColumns(t *testing.T) {
	m := catalogue(t)
	typed := typeInto(t, m, "SELECT * FROM catalog.products WHERE ")
	answered, _ := typed.Update(runFirst(t, typed.readColumns()))
	ready := answered.(Model)

	deep := typeInto(t, ready, "catalog.products.sk")
	if item, ok := deep.suggest.selected(); !ok || item.text != "catalog.products.sku" {
		t.Errorf("suggestion = %+v", deep.suggest.items)
	}
}

// A dozen tables start with the same word. The one being typed is the one with
// the least left to type, and a list cut in the server's order loses it.
func TestTheClosestTableIsOffered(t *testing.T) {
	conn := healthy()
	conn.tables = nil
	for _, name := range []string{
		"product_attributes", "product_changes", "product_components",
		"product_descriptions", "product_images", "product_import_records",
		"product_list_prices", "products",
	} {
		conn.tables = append(conn.tables,
			driver.Table{Schema: "catalog", Name: name, Kind: "table"})
	}
	m := loadedWith(t, conn, workspaceWith(t))
	m.width, m.height = 120, 40
	editing, _ := press(t, m, "e")
	editing.editor.SetValue("SELECT id FROM catalog.product")
	typed, _ := editing.resuggest()
	if !typed.suggest.active() {
		t.Fatal("a prefix that names eight tables must offer some")
	}
	if got := typed.suggest.items[0].text; got != "catalog.products" {
		t.Errorf("the closest match must come first, got %q from %v",
			got, texts(typed.suggest.items))
	}
}

func texts(items []suggestion) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		out = append(out, item.text)
	}
	return out
}

// Every other binding in this program answers to a letter as well. A letter
// typed into an editor is a letter, whatever list happens to be open.
//
// A textarea shares one buffer between every copy of itself, so each case gets
// a model of its own rather than the last one's leftovers.
func TestALetterTypedAtASuggestionListIsALetter(t *testing.T) {
	suggesting := func() Model {
		t.Helper()
		m := loadedWith(t, healthy(), workspaceWith(t))
		m.width, m.height = 120, 40
		editing, _ := press(t, m, "e")
		typed := editing
		for _, key := range []string{"s"} {
			typed, _ = press(t, typed, key)
		}
		if !typed.suggest.active() {
			t.Fatal("the list must be open for this to be worth testing")
		}
		return typed
	}
	for _, key := range []string{"k", "j", "h", "l"} {
		typed := suggesting()
		after, _ := press(t, typed, key)
		if after.suggest.cursor != typed.suggest.cursor {
			t.Errorf("%q walked the list instead of being typed", key)
		}
		if !strings.HasSuffix(after.editor.Value(), key) {
			t.Errorf("%q must reach the editor, got %q", key, after.editor.Value())
		}
	}
	typed := suggesting()
	if walked, _ := press(t, typed, "down"); walked.suggest.cursor == typed.suggest.cursor {
		t.Error("the arrows still walk the list")
	}
}

// The screen says how to take a suggestion, and how many other ways there are
// to finish the word.
func TestTheScreenSaysHowToTakeASuggestion(t *testing.T) {
	theme := ui.Default()
	one := completion{items: []suggestion{{text: "users"}}, prefix: "us"}
	if hint := one.hint(theme); !strings.Contains(plain(hint), "tab to accept") {
		t.Errorf("hint = %q", plain(hint))
	}
	if strings.Contains(plain(one.hint(theme)), "other way") {
		t.Error("there is only one way to finish it")
	}
	several := completion{
		items:  []suggestion{{text: "users"}, {text: "usage"}},
		prefix: "us",
	}
	if hint := plain(several.hint(theme)); !strings.Contains(hint, "other ways") {
		t.Errorf("hint = %q, the arrows must be offered", hint)
	}
}

// Nothing is drawn after the cursor when there is no cursor to draw after.
func TestNothingIsDrawnAfterACursorThatIsNotThere(t *testing.T) {
	m := typeInto(t, withColumns(t), "SELECT * FROM us")
	lines := strings.Split(m.editor.View(), "\n")
	elsewhere := m
	elsewhere.focus = focusResults
	if got := elsewhere.withGhost(lines); len(got) != len(lines) || got[0] != lines[0] {
		t.Error("a suggestion belongs to the editor, and only while it has the cursor")
	}
	if got := m.withGhost(nil); got != nil {
		t.Error("there is no line to draw it on")
	}
}
