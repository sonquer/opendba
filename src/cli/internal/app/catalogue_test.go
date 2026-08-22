package app

import (
	"strings"
	"testing"
	"time"

	"github.com/sonquer/tui4db/src/cli/internal/driver"
	"github.com/sonquer/tui4db/src/cli/internal/ui"
)

func hotTable() driver.Table {
	return driver.Table{
		Schema: "catalog", Name: "products", Kind: "table",
		Rows: 1463, Size: 1 << 20, Comment: "everything for sale",
		Stats: true, IndexScans: 9800, SeqScans: 200,
		LiveRows: 1400, DeadRows: 63, CacheHit: 0.999,
		IndexSize: 1 << 18, LastVacuum: time.Now().Add(-3 * time.Hour),
	}
}

func pageOf(t *testing.T, table driver.Table) string {
	t.Helper()
	m := loaded(t, healthy())
	m.width, m.height = 110, 40
	return flat(plain(m.tablePage(table).view(m.width, 60)))
}

// flat puts a wrapped page back on one line, without the border it is wrapped
// in, because what a page says is not where the renderer decided to break it.
func flat(page string) string {
	return strings.Join(strings.Fields(strings.ReplaceAll(page, "│", "")), " ")
}

func TestATablePageSaysWhatTheNumbersMean(t *testing.T) {
	page := pageOf(t, hotTable())
	for _, want := range []string{
		"catalog.products", "1,463", "1.0 MiB", "read by index", "9,800",
		"dead rows", "63", "vacuumed", "3 hours ago",
		"HOW IT IS READ", "Almost every read", "WHAT IS IN IT", "WHERE IT IS READ FROM",
		"99.9%", ui.BarStyleNamed(ui.DefaultBarStyle).Full,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("the page must say %q:\n%s", want, page)
		}
	}
}

func TestATablePageForEveryWayATableCanBeRead(t *testing.T) {
	cases := []struct {
		name  string
		table driver.Table
		want  string
	}{
		{"nothing has read it", driver.Table{Name: "cold", Stats: true}, "Nothing has read this table"},
		{"half and half", driver.Table{Name: "mixed", Stats: true, Size: 1 << 20,
			IndexScans: 60, SeqScans: 40}, "the planner being right"},
		{"walked end to end", driver.Table{Name: "walked", Stats: true, Size: 1 << 26,
			IndexScans: 10, SeqScans: 90}, "walk the whole table"},
		{"no counters at all", driver.Table{Name: "plain"}, "keeps no counters"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := pageOf(t, c.table); !strings.Contains(got, c.want) {
				t.Errorf("the page must say %q:\n%s", c.want, got)
			}
		})
	}
}

func TestATablePageOnDeadRows(t *testing.T) {
	cases := []struct {
		name string
		dead int64
		want string
	}{
		{"normal", 2, "is normal for a table"},
		{"watching", 15, "worth watching"},
		{"acting", 40, "one read in five"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			table := driver.Table{Name: "queue", Stats: true, LiveRows: 100, DeadRows: c.dead}
			if got := pageOf(t, table); !strings.Contains(got, c.want) {
				t.Errorf("the page must say %q:\n%s", c.want, got)
			}
		})
	}
	never := driver.Table{Name: "queue", Stats: true, LiveRows: 100}
	if got := pageOf(t, never); !strings.Contains(got, "Nothing has vacuumed this table yet") {
		t.Errorf("a table nothing has vacuumed says so:\n%s", got)
	}
}

func TestATablePageOnMemory(t *testing.T) {
	cold := driver.Table{Name: "logs", Stats: true, LiveRows: 1, CacheHit: 0.6}
	if got := pageOf(t, cold); !strings.Contains(got, "the rest came from disk") {
		t.Errorf("a cold table says where its reads come from:\n%s", got)
	}
}

func TestAnIndexPageSaysWhatItIsFor(t *testing.T) {
	m := loaded(t, healthy())
	m.width, m.height = 110, 40
	cases := []struct {
		name  string
		index driver.Index
		want  string
	}{
		{"primary", driver.Index{Schema: "catalog", Name: "products_pkey", Table: "products",
			Primary: true, Unique: true, Stats: true}, "how every row of the table is identified"},
		{"idle", driver.Index{Schema: "catalog", Name: "products_old", Table: "products",
			Size: 1 << 20, Stats: true}, "a tax on every write"},
		{"unique", driver.Index{Schema: "catalog", Name: "products_sku", Table: "products",
			Unique: true, Scans: 4, Stats: true}, "refuses duplicates"},
		{"used", driver.Index{Schema: "catalog", Name: "products_name", Table: "products",
			Scans: 12, Rows: 400, Stats: true}, "without walking the whole table"},
		{"no counters", driver.Index{Name: "products_name", Table: "products"}, "keeps no counters"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			page := flat(plain(m.indexPage(c.index).view(m.width, 60)))
			if !strings.Contains(page, c.want) {
				t.Errorf("the page must say %q:\n%s", c.want, page)
			}
			if !strings.Contains(page, c.index.Name) {
				t.Errorf("the page must name the index:\n%s", page)
			}
		})
	}
}

func TestTheColumnsReachAnOpenTablePage(t *testing.T) {
	m := loaded(t, healthy())
	m.width, m.height = 110, 40
	list, _ := press(t, m, "s")
	opened, _ := press(t, list, "enter")
	if opened.page == nil {
		t.Fatal("enter must open the table")
	}
	if strings.Contains(plain(opened.page.view(110, 40)), "email") {
		t.Fatal("the columns are read after the page opens")
	}
	filled, _ := opened.Update(columnsMsg{
		table:   "users",
		columns: []driver.Column{{Name: "email", Type: "text", PrimaryKey: true}},
	})
	page := filled.(Model).page
	if page == nil || !strings.Contains(plain(page.view(110, 40)), "email") {
		t.Errorf("the columns must reach the page that asked for them")
	}
}

func TestAPageForAnotherTableIsLeftAlone(t *testing.T) {
	m := loaded(t, healthy())
	m.width, m.height = 110, 40
	list, _ := press(t, m, "s")
	opened, _ := press(t, list, "enter")
	other, _ := opened.Update(columnsMsg{table: "orders"})
	if other.(Model).page == nil || other.(Model).page.title != "main.users" {
		t.Error("columns for another table must not rebuild this page")
	}
	closed := opened
	closed.page = nil
	if again := closed.repage("users"); again.page != nil {
		t.Error("there is no page to rebuild")
	}
}

func TestHowLongAgo(t *testing.T) {
	now := time.Now()
	cases := map[string]time.Time{
		"just now":     now.Add(-2 * time.Second),
		"9 minutes":    now.Add(-9 * time.Minute),
		"5 hours":      now.Add(-5 * time.Hour),
		"3 days":       now.Add(-73 * time.Hour),
		"1 minute ago": now.Add(-time.Minute),
	}
	for want, when := range cases {
		if got := ago(when); !strings.Contains(got, want) {
			t.Errorf("ago(%v) = %q, want %q", when, got, want)
		}
	}
}

func TestAListingPageNeedsARowToBeOn(t *testing.T) {
	m := loaded(t, healthy())
	m.width, m.height = 110, 40
	for _, screen := range []view{viewSchema, viewIndexes} {
		out := m
		out.view = screen
		out.listing = 99
		empty, cmd := out.listingPage()
		if cmd != nil || empty.(Model).page != nil {
			t.Errorf("%s has no row 99 to open", screen)
		}
	}
}

func TestTheVerdictOnATableIsTheThemesToRender(t *testing.T) {
	theme := ui.Default()
	if got := theme.Severity4Driver(hotTable().Health()); got != ui.SevOK {
		t.Errorf("a warm table read through its indexes is fine: %v", got)
	}
}
