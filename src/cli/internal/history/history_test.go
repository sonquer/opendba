package history

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/sonquer/opendba/src/cli/internal/config"
)

func settings() config.HistorySettings {
	return config.HistorySettings{Enabled: true, StoreSQL: true, Limit: 100}
}

func store(t *testing.T, options config.HistorySettings) *Store {
	t.Helper()
	path := filepath.Join(t.TempDir(), "history.db")
	opened, err := Open(path, options)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = opened.Close() })
	return opened
}

func entry(statement string) Entry {
	return Entry{
		ConnectionID:   "01J",
		ConnectionName: "production-eu",
		Statement:      statement,
		Kind:           "SELECT",
		Rows:           3,
		Duration:       42 * time.Millisecond,
	}
}

func TestRecordAndRead(t *testing.T) {
	history := store(t, settings())
	ctx := context.Background()
	if err := history.Record(ctx, entry("SELECT 1")); err != nil {
		t.Fatalf("Record: %v", err)
	}
	entries, err := history.Recent(ctx, "", 10)
	if err != nil {
		t.Fatalf("Recent: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("entries = %+v", entries)
	}
	saved := entries[0]
	if saved.Statement != "SELECT 1" || saved.ConnectionName != "production-eu" {
		t.Fatalf("entry = %+v", saved)
	}
	if saved.Duration != 42*time.Millisecond || saved.Rows != 3 {
		t.Errorf("entry = %+v", saved)
	}
	if saved.RanAt.IsZero() || saved.RanAt.After(time.Now().Add(time.Second)) {
		t.Errorf("ran at = %v", saved.RanAt)
	}
	if !saved.Succeeded() {
		t.Error("a query without a failure succeeded")
	}
}

func TestRecordKeepsFailures(t *testing.T) {
	history := store(t, settings())
	ctx := context.Background()
	failed := entry("SELECT * FROM missing")
	failed.Failure = "relation does not exist"
	if err := history.Record(ctx, failed); err != nil {
		t.Fatalf("Record: %v", err)
	}
	entries, err := history.Recent(ctx, "01J", 10)
	if err != nil {
		t.Fatalf("Recent: %v", err)
	}
	if len(entries) != 1 || entries[0].Succeeded() {
		t.Fatalf("entries = %+v", entries)
	}
}

func TestHistoryCanBeTurnedOff(t *testing.T) {
	options := settings()
	options.Enabled = false
	history := store(t, options)
	ctx := context.Background()
	if err := history.Record(ctx, entry("SELECT 1")); err != nil {
		t.Fatalf("Record: %v", err)
	}
	count, err := history.Count(ctx)
	if err != nil {
		t.Fatalf("Count: %v", err)
	}
	if count != 0 {
		t.Errorf("nothing must be recorded, got %d entries", count)
	}
}

func TestStatementsCanBeKeptOut(t *testing.T) {
	options := settings()
	options.StoreSQL = false
	history := store(t, options)
	ctx := context.Background()
	if err := history.Record(ctx, entry("SELECT secret FROM vault")); err != nil {
		t.Fatalf("Record: %v", err)
	}
	entries, err := history.Recent(ctx, "", 10)
	if err != nil {
		t.Fatalf("Recent: %v", err)
	}
	if entries[0].Statement != redactedStatement {
		t.Fatalf("the statement must not be stored: %q", entries[0].Statement)
	}
	if entries[0].Kind != "SELECT" || entries[0].Duration == 0 {
		t.Errorf("the timing must still be kept: %+v", entries[0])
	}
}

func TestRecentIsFilteredByConnection(t *testing.T) {
	history := store(t, settings())
	ctx := context.Background()
	first := entry("SELECT 1")
	second := entry("SELECT 2")
	second.ConnectionID = "01K"
	for _, e := range []Entry{first, second} {
		if err := history.Record(ctx, e); err != nil {
			t.Fatal(err)
		}
	}
	entries, err := history.Recent(ctx, "01K", 10)
	if err != nil {
		t.Fatalf("Recent: %v", err)
	}
	if len(entries) != 1 || entries[0].Statement != "SELECT 2" {
		t.Fatalf("entries = %+v", entries)
	}
}

func TestNewestFirst(t *testing.T) {
	history := store(t, settings())
	ctx := context.Background()
	base := time.Now().Add(-time.Hour)
	for i, statement := range []string{"SELECT 1", "SELECT 2", "SELECT 3"} {
		e := entry(statement)
		e.RanAt = base.Add(time.Duration(i) * time.Minute)
		if err := history.Record(ctx, e); err != nil {
			t.Fatal(err)
		}
	}
	entries, err := history.Recent(ctx, "", 10)
	if err != nil {
		t.Fatalf("Recent: %v", err)
	}
	if len(entries) != 3 || entries[0].Statement != "SELECT 3" {
		t.Fatalf("entries = %+v", entries)
	}
}

func TestSearch(t *testing.T) {
	history := store(t, settings())
	ctx := context.Background()
	for _, statement := range []string{"SELECT id FROM users", "SELECT id FROM orders"} {
		if err := history.Record(ctx, entry(statement)); err != nil {
			t.Fatal(err)
		}
	}
	entries, err := history.Search(ctx, "orders", 10)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(entries) != 1 || !strings.Contains(entries[0].Statement, "orders") {
		t.Fatalf("entries = %+v", entries)
	}
}

func TestFavourites(t *testing.T) {
	history := store(t, settings())
	ctx := context.Background()
	if err := history.Record(ctx, entry("SELECT 1")); err != nil {
		t.Fatal(err)
	}
	entries, err := history.Recent(ctx, "", 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := history.SetFavorite(ctx, entries[0].ID, true); err != nil {
		t.Fatalf("SetFavorite: %v", err)
	}
	favorites, err := history.Favorites(ctx, 0)
	if err != nil {
		t.Fatalf("Favorites: %v", err)
	}
	if len(favorites) != 1 || !favorites[0].Favorite {
		t.Fatalf("favorites = %+v", favorites)
	}
	if err := history.SetFavorite(ctx, entries[0].ID, false); err != nil {
		t.Fatal(err)
	}
	favorites, err = history.Favorites(ctx, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(favorites) != 0 {
		t.Fatalf("favorites = %+v", favorites)
	}
}

func TestTrimKeepsTheLimitAndTheFavourites(t *testing.T) {
	options := settings()
	options.Limit = 3
	history := store(t, options)
	ctx := context.Background()

	oldest := entry("SELECT oldest")
	oldest.RanAt = time.Now().Add(-time.Hour)
	if err := history.Record(ctx, oldest); err != nil {
		t.Fatal(err)
	}
	entries, err := history.Recent(ctx, "", 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := history.SetFavorite(ctx, entries[0].ID, true); err != nil {
		t.Fatal(err)
	}

	for i := 0; i < 6; i++ {
		if err := history.Record(ctx, entry("SELECT filler")); err != nil {
			t.Fatal(err)
		}
	}
	count, err := history.Count(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if count > 4 {
		t.Errorf("the history must stay near its limit, got %d entries", count)
	}
	favorites, err := history.Favorites(ctx, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(favorites) != 1 {
		t.Error("a favourite must survive trimming")
	}
}

func TestUnlimitedHistoryIsNotTrimmed(t *testing.T) {
	options := settings()
	options.Limit = 0
	history := store(t, options)
	ctx := context.Background()
	for i := 0; i < 5; i++ {
		if err := history.Record(ctx, entry("SELECT 1")); err != nil {
			t.Fatal(err)
		}
	}
	count, err := history.Count(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if count != 5 {
		t.Errorf("count = %d, want 5", count)
	}
}

func TestClear(t *testing.T) {
	history := store(t, settings())
	ctx := context.Background()
	if err := history.Record(ctx, entry("SELECT 1")); err != nil {
		t.Fatal(err)
	}
	if err := history.Clear(ctx); err != nil {
		t.Fatalf("Clear: %v", err)
	}
	count, err := history.Count(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Errorf("count = %d", count)
	}
}

func TestTheHistoryFileIsPrivate(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permissions are not enforced on windows")
	}
	path := filepath.Join(t.TempDir(), "nested", "history.db")
	opened, err := Open(path, settings())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer opened.Close()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("mode = %04o, want 0600", info.Mode().Perm())
	}
}

func TestOpenFailures(t *testing.T) {
	blocked := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(blocked, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(filepath.Join(blocked, "nested", "history.db"), settings()); err == nil {
		t.Error("an unusable directory must be reported")
	}
	if _, err := Open(t.TempDir(), settings()); err == nil {
		t.Error("a directory cannot be a history file")
	}
}

func TestEveryCallReportsAClosedStore(t *testing.T) {
	history := store(t, settings())
	if err := history.Close(); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	calls := map[string]func() error{
		"Record":      func() error { return history.Record(ctx, entry("SELECT 1")) },
		"Recent":      func() error { _, err := history.Recent(ctx, "", 10); return err },
		"Search":      func() error { _, err := history.Search(ctx, "x", 10); return err },
		"Favorites":   func() error { _, err := history.Favorites(ctx, 10); return err },
		"SetFavorite": func() error { return history.SetFavorite(ctx, 1, true) },
		"Clear":       func() error { return history.Clear(ctx) },
		"Count":       func() error { _, err := history.Count(ctx); return err },
	}
	for name, call := range calls {
		if err := call(); err == nil {
			t.Errorf("%s must report the closed history", name)
		}
	}
}

func TestSnippet(t *testing.T) {
	e := entry("SELECT\n\tid,\n\temail\nFROM users")
	if got := e.Snippet(0); got != "SELECT id, email FROM users" {
		t.Errorf("Snippet(0) = %q", got)
	}
	if got := e.Snippet(10); got != "SELECT id…" {
		t.Errorf("Snippet(10) = %q", got)
	}
}

func TestPageSize(t *testing.T) {
	if pageSize(0) != 50 || pageSize(-1) != 50 || pageSize(7) != 7 {
		t.Error("an unset page size must fall back to a sane default")
	}
}

func TestListReportsScanFailures(t *testing.T) {
	history := store(t, settings())
	ctx := context.Background()
	if _, err := history.list(ctx, "SELECT 1"); err == nil {
		t.Fatal("a result that does not match the entry shape must be reported")
	}
}

func TestRecordFailsWhenTheTableIsGone(t *testing.T) {
	history := store(t, settings())
	ctx := context.Background()
	if _, err := history.db.ExecContext(ctx, "DROP TABLE queries"); err != nil {
		t.Fatal(err)
	}
	if err := history.Record(ctx, entry("SELECT 1")); err == nil {
		t.Fatal("recording into a missing table must fail")
	}
	if err := history.trim(ctx); err == nil {
		t.Fatal("trimming a missing table must fail")
	}
}
