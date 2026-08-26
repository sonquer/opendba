package app

import (
	"context"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sonquer/opendba/src/cli/internal/export"
	"github.com/sonquer/opendba/src/cli/internal/sqlfiles"
)

// exporting drives the dialog to the end and hands back the model and the path
// it was told to write.
func exporting(t *testing.T, m Model, format string) (Model, string) {
	t.Helper()
	opened, _ := m.Update(exportMsg{})
	dialog := opened.(Model)
	if dialog.exporter == nil {
		t.Fatal("the export command must raise the question")
	}
	path := filepath.Join(t.TempDir(), "result."+format)
	fields := append([]field(nil), dialog.exporter.form.fields...)
	for i := range fields {
		switch fields[i].key {
		case "path":
			fields[i].input.SetValue(path)
		case "format":
			for at, choice := range fields[i].choices {
				if choice == format {
					fields[i].choice = at
				}
			}
		}
	}
	dialog.exporter.form.fields = fields
	return dialog, path
}

func writeIt(t *testing.T, m Model) Model {
	t.Helper()
	started, cmd := m.startExport()
	if cmd == nil {
		t.Fatal("writing the file must start something")
	}
	return settle(t, started.(Model), cmd)
}

func ranSomething(t *testing.T) Model {
	t.Helper()
	m := typeInto(t, workbench(t), "SELECT * FROM users")
	ran, cmd := press(t, m, "ctrl+r")
	return settle(t, ran, cmd)
}

// Every format writes a file, and what is in it is what the driver returned
// rather than what the table drew.
func TestEveryFormatWritesAFile(t *testing.T) {
	for _, format := range export.Formats() {
		t.Run(string(format), func(t *testing.T) {
			dialog, path := exporting(t, ranSomething(t), string(format))
			done := writeIt(t, dialog)
			if done.exporter != nil {
				t.Fatalf("the dialog must close once the file is written: %+v",
					done.exporter.trouble)
			}
			written, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("the file must be there: %v", err)
			}
			if len(written) == 0 {
				t.Error("and must not be empty")
			}
			if format != export.FormatXLSX && !strings.Contains(string(written), "email") {
				t.Errorf("it must hold the columns:\n%s", written)
			}
		})
	}
}

// A file is written where it was asked for, with nothing left beside it.
func TestNothingIsLeftBesideTheFile(t *testing.T) {
	dialog, path := exporting(t, ranSomething(t), "csv")
	writeIt(t, dialog)
	beside, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(beside) != 1 {
		t.Errorf("the directory must hold the file and nothing else: %v", beside)
	}
}

// Nothing is written over. Losing yesterday's export to today's is not a thing
// a program that will not write to your database should do to your disk.
func TestAFileThatIsAlreadyThereIsNotWrittenOver(t *testing.T) {
	dialog, path := exporting(t, ranSomething(t), "csv")
	if err := os.WriteFile(path, []byte("keep me"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	done := writeIt(t, dialog)
	if done.exporter == nil || done.exporter.trouble == "" {
		t.Fatal("the dialog must stay open and say why")
	}
	if !strings.Contains(done.exporter.trouble, "already there") {
		t.Errorf("trouble = %q", done.exporter.trouble)
	}
	held, err := os.ReadFile(path)
	if err != nil || string(held) != "keep me" {
		t.Errorf("the file that was there must be untouched: %q %v", held, err)
	}
}

// A statement that changes data is not run a second time, so the only scope it
// offers is what is already in memory.
func TestAWriteIsNotRunAgainToExportIt(t *testing.T) {
	m := loadedWith(t, healthy(), workspaceWith(t))
	m.session.Connection.Mode = "read-write"
	editing, _ := press(t, m, "e")
	typed := typeInto(t, editing, "DELETE FROM users")
	typed.results = newResults(typed.theme, queriedMsg{
		statement: "DELETE FROM users",
		columns:   []string{"id"},
		rows:      [][]any{{int64(1)}},
	}, 60, 6)

	opened, _ := typed.Update(exportMsg{})
	dialog := opened.(Model).exporter
	if dialog == nil {
		t.Fatal("the question is still asked")
	}
	if dialog.reads {
		t.Error("a statement that changes data must not be run again")
	}
	if got := dialog.scopes(); len(got) != 1 || got[0] != scopeOnScreen {
		t.Errorf("scopes = %v, there is only one honest answer", got)
	}
	if dialog.refusal == "" {
		t.Error("and the dialog has to say why")
	}
}

// A read offers both scopes, and everything is the one it starts on.
func TestAReadOffersEverythingFirst(t *testing.T) {
	opened, _ := ranSomething(t).Update(exportMsg{})
	dialog := opened.(Model).exporter
	if !dialog.reads {
		t.Fatal("a select can be run again")
	}
	if got := dialog.scopes(); len(got) != 2 || got[0] != scopeEverything {
		t.Errorf("scopes = %v, everything is the default", got)
	}
	if dialog.form.value("scope") != scopeEverything {
		t.Errorf("scope = %q", dialog.form.value("scope"))
	}
}

// Everything reads past the cap the drawn result stopped at.
func TestEverythingReadsPastTheCap(t *testing.T) {
	conn := healthy()
	dialog, path := exporting(t, ranSomething(t), "csv")
	dialog.exporter.rows = nil
	writeIt(t, dialog)
	written, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !strings.Contains(string(written), "a@example.com") {
		t.Errorf("the rows must come from the server rather than the screen:\n%s", written)
	}
	if conn.counted()["stream"] > 0 {
		t.Error("a fake counts what it was asked for")
	}
}

// Nothing is exported before anything has run.
func TestThereIsNothingToExportBeforeAnythingHasRun(t *testing.T) {
	m := workbench(t)
	refused, cmd := m.Update(exportMsg{})
	if refused.(Model).exporter != nil {
		t.Error("there is no dialog about a result that is not there")
	}
	if cmd == nil {
		t.Error("and it has to say so")
	}
}

// Choosing a format follows the file name, so a spreadsheet is not written to
// something called .csv.
func TestTheFileNameFollowsTheFormat(t *testing.T) {
	opened, _ := ranSomething(t).Update(exportMsg{})
	m := opened.(Model)
	if !strings.HasSuffix(m.exporter.form.value("path"), ".csv") {
		t.Fatalf("path = %q", m.exporter.form.value("path"))
	}
	moved, _ := press(t, m, "right")
	if suffix := filepath.Ext(moved.exporter.form.value("path")); suffix != ".xlsx" {
		t.Errorf("suffix = %q, the name must follow the format", suffix)
	}
}

// Esc closes the question without writing anything.
func TestEscapeLeavesTheExportAlone(t *testing.T) {
	opened, _ := ranSomething(t).Update(exportMsg{})
	closed, _ := press(t, opened.(Model), "esc")
	if closed.exporter != nil {
		t.Error("esc must close the dialog")
	}
}

// A name that would be a path is not one.
func TestATabNameDoesNotBecomeADirectory(t *testing.T) {
	for _, want := range []struct {
		name, cleaned string
	}{
		{"catalog.product_prices", "catalog.product_prices"},
		{"a/b", "a-b"},
		{"a b", "a-b"},
		{`a"b`, "a-b"},
		{"-edges-", "edges"},
	} {
		t.Run(want.name, func(t *testing.T) {
			if got := sqlfiles.Clean(want.name); got != want.cleaned {
				t.Errorf("sqlfiles.Clean(%q) = %q, want %q", want.name, got, want.cleaned)
			}
		})
	}
}

// The dialog says what it is doing: how far it has got, what went wrong, and
// why a statement that changes data will not be run again.
func TestTheExportDialogSaysWhatItIsDoing(t *testing.T) {
	opened, _ := ranSomething(t).Update(exportMsg{})
	m := opened.(Model)
	view := plain(m.exporter.view(110, 32))
	for _, want := range []string{"export the result", "format", "file", "rows", "write the file"} {
		if !strings.Contains(view, want) {
			t.Errorf("the dialog must show %q:\n%s", want, view)
		}
	}

	busy := *m.exporter
	busy.busy, busy.rowsSeen = true, 4000
	if drawn := plain(busy.view(110, 32)); !strings.Contains(drawn, "4000 rows so far") ||
		!strings.Contains(drawn, "stop writing") {
		t.Errorf("a file being written must say how far it has got:\n%s", drawn)
	}

	failed := *m.exporter
	failed.trouble = "the disk is full"
	if drawn := plain(failed.view(110, 32)); !strings.Contains(drawn, "the disk is full") {
		t.Errorf("and must say what stopped it:\n%s", drawn)
	}

	refused := *m.exporter
	refused.refusal = "this statement changes data"
	if drawn := plain(refused.view(110, 32)); !strings.Contains(drawn, "changes data") {
		t.Errorf("and why it will not run the statement again:\n%s", drawn)
	}
}

// A long export is visibly moving, and giving up on it stops it.
func TestALongExportIsCountedAndCanBeStopped(t *testing.T) {
	conn := healthy()
	conn.holdQuery = make(chan struct{})
	defer close(conn.holdQuery)

	m := loadedWith(t, conn, workspaceWith(t))
	editing, _ := press(t, m, "e")
	typed := typeInto(t, editing, "SELECT * FROM users")
	typed.results = newResults(typed.theme, queriedMsg{
		statement: "SELECT * FROM users",
		columns:   []string{"id"},
		rows:      [][]any{{int64(1)}},
	}, 60, 6)

	dialog, _ := exporting(t, typed, "csv")
	started, cmd := dialog.startExport()
	running := started.(Model)
	if !running.exporter.busy || running.stopExport == nil || cmd == nil {
		t.Fatal("writing must be something that is happening and can be stopped")
	}

	moved, _ := running.Update(exportProgressMsg{rows: 8000, token: running.exporter.token})
	if moved.(Model).exporter.rowsSeen != 8000 {
		t.Errorf("rows = %d, a long export has to be visibly moving",
			moved.(Model).exporter.rowsSeen)
	}
	stale, _ := moved.(Model).Update(exportProgressMsg{rows: 1, token: -1})
	if stale.(Model).exporter.rowsSeen != 8000 {
		t.Error("and a count from an export that was left behind is not it")
	}

	writing := running.exporter.running
	stopped, _ := press(t, moved.(Model), "esc")
	if stopped.exporter != nil || stopped.stopExport != nil {
		t.Error("esc must give up on the file")
	}
	ended := waitFor4Export(t, writing)
	if ended.err == nil {
		t.Error("a file that was given up on must say it did not finish")
	}
}

// waitFor4Export waits for the goroutine writing a file to stop, so a test does
// not finish while something is still writing into the directory it is about to
// take away.
func waitFor4Export(t *testing.T, writing running4Export) finished4Export {
	t.Helper()
	for range 100 {
		select {
		case ended := <-writing.done:
			return ended
		case <-writing.progress:
		case <-time.After(50 * time.Millisecond):
		}
	}
	t.Fatal("the export never stopped")
	return finished4Export{}
}

// A file that could not be written leaves the dialog open with the reason.
func TestAnExportThatFailsSaysSoAndStaysOpen(t *testing.T) {
	conn := healthy()
	conn.failOn = "stream"
	m := loadedWith(t, conn, workspaceWith(t))
	editing, _ := press(t, m, "e")
	typed := typeInto(t, editing, "SELECT * FROM users")
	typed.results = newResults(typed.theme, queriedMsg{
		statement: "SELECT * FROM users",
		columns:   []string{"id"},
		rows:      [][]any{{int64(1)}},
	}, 60, 6)

	dialog, path := exporting(t, typed, "csv")
	done := writeIt(t, dialog)
	if done.exporter == nil || done.exporter.trouble == "" {
		t.Fatal("a failure must be said rather than swallowed")
	}
	if done.exporter.busy {
		t.Error("and it must stop saying it is writing")
	}
	if _, err := os.Stat(path); err == nil {
		t.Error("nothing that is not whole is left on the disk")
	}
}

// A format that does not exist and a file with no name are both refused before
// anything is written.
func TestAnExportThatCannotStartIsRefused(t *testing.T) {
	for _, want := range []struct {
		name    string
		break4  func(*exporter)
		trouble string
	}{
		{"no file name", func(e *exporter) { set4Export(e, "path", "") }, "cannot be empty"},
		{"no such format", func(e *exporter) { set4Export(e, "format", "parquet") }, "no such format"},
	} {
		t.Run(want.name, func(t *testing.T) {
			dialog, _ := exporting(t, ranSomething(t), "csv")
			fields := append([]field(nil), dialog.exporter.form.fields...)
			for i := range fields {
				if fields[i].key == "path" {
					fields[i].required = true
				}
			}
			dialog.exporter.form.fields = fields
			want.break4(dialog.exporter)
			refused, cmd := dialog.startExport()
			if cmd != nil {
				t.Error("nothing must start")
			}
			if got := refused.(Model).exporter.trouble; !strings.Contains(got, want.trouble) {
				t.Errorf("trouble = %q, want something about %q", got, want.trouble)
			}
		})
	}
}

func set4Export(e *exporter, key, value string) {
	fields := append([]field(nil), e.form.fields...)
	for i := range fields {
		if fields[i].key != key {
			continue
		}
		if fields[i].kind == fieldChoice {
			fields[i].choices = append(fields[i].choices, value)
			fields[i].choice = len(fields[i].choices) - 1
			continue
		}
		fields[i].input.SetValue(value)
	}
	e.form.fields = fields
}

// A value the chosen format cannot hold stops the export, whichever rows were
// being written.
func TestAValueTheFormatCannotHoldStopsTheExport(t *testing.T) {
	for _, scope := range []string{scopeOnScreen, scopeEverything} {
		t.Run(scope, func(t *testing.T) {
			m := ranSomething(t)
			m.results.rows = [][]any{{math.Inf(1)}}
			m.results.columns = []string{"ratio"}
			dialog, path := exporting(t, m, "json")
			set4Export(dialog.exporter, "scope", scope)
			dialog.exporter.rows = [][]any{{math.Inf(1)}}
			dialog.exporter.columns = []string{"ratio"}
			dialog.exporter.statement = "SELECT 1"
			done := writeIt(t, dialog)
			if scope == scopeOnScreen {
				if done.exporter == nil || done.exporter.trouble == "" {
					t.Fatal("a value that cannot be written must stop the file")
				}
				if _, err := os.Stat(path); err == nil {
					t.Error("and leave nothing behind")
				}
			}
		})
	}
}

// While a file is being written the dialog takes no other keys, because a form
// that can be edited underneath a write is a form that lies about what is being
// written.
func TestTheDialogIsReadOnlyWhileItIsWriting(t *testing.T) {
	m := ranSomething(t)
	opened, _ := m.Update(exportMsg{})
	dialog := opened.(Model)
	busy := *dialog.exporter
	busy.busy = true
	dialog.exporter = &busy
	before := dialog.exporter.form.value("format")

	typed, cmd := press(t, dialog, "right")
	if cmd != nil {
		t.Error("nothing happens")
	}
	if typed.exporter.form.value("format") != before {
		t.Error("and the form does not change under the file being written")
	}
}

// A file with no extension keeps its name when the format changes, because it
// was typed by hand and guessing at it would be rude.
func TestAFileWithNoExtensionKeepsItsName(t *testing.T) {
	opened, _ := ranSomething(t).Update(exportMsg{})
	m := opened.(Model)
	set4Export(m.exporter, "path", "rows")
	moved, _ := press(t, m, "right")
	if got := moved.exporter.form.value("path"); got != "rows" {
		t.Errorf("path = %q, a name with no extension is left alone", got)
	}
}

// A file that cannot be made at all is said rather than half written.
func TestAFileThatCannotBeMadeIsRefused(t *testing.T) {
	written, err := write4Export(context.Background(), job4Export{
		path:     filepath.Join(t.TempDir(), "no", "such", "place.csv"),
		format:   export.FormatCSV,
		columns:  []string{"a"},
		rows:     [][]any{{1}},
		progress: make(chan int, 1),
	})
	if err == nil {
		t.Error("a directory that is not there is not somewhere to write")
	}
	if written != 0 {
		t.Errorf("written = %d, nothing was", written)
	}
}

// Saying how far a file has got never waits to be heard, because an export that
// stopped because nobody was counting would be an export lost to a dialog
// somebody closed.
func TestSayingHowFarItHasGotNeverWaits(t *testing.T) {
	job := job4Export{progress: make(chan int)}
	job.tell(context.Background(), 1)
	job.tell(context.Background(), tellEvery)

	stopped, cancel := context.WithCancel(context.Background())
	cancel()
	job.tell(stopped, tellEvery)

	heard := make(chan int, 1)
	listening := job4Export{progress: heard}
	listening.tell(context.Background(), tellEvery)
	select {
	case got := <-heard:
		if got != tellEvery {
			t.Errorf("heard %d", got)
		}
	default:
		t.Error("somebody counting must be told")
	}
}

// An export says what it is about to do before it does it, because it is the
// one thing this program does that reaches outside itself.
func TestAnExportAsksBeforeItWrites(t *testing.T) {
	for _, want := range []struct {
		name  string
		scope string
		said  []string
		warns bool
	}{
		{
			name:  "everything",
			scope: scopeEverything,
			said:  []string{"write this file?", "every row the statement returns"},
			warns: true,
		},
		{
			name:  "what is on screen",
			scope: scopeOnScreen,
			said:  []string{"write this file?", "already read are written"},
		},
	} {
		t.Run(want.name, func(t *testing.T) {
			m := ranSomething(t)
			m.width, m.height = 110, 36
			dialog, _ := exporting(t, m, "csv")
			set4Export(dialog.exporter, "scope", want.scope)

			asked, _ := dialog.confirmExport()
			question := asked.(Model)
			if question.modal == nil {
				t.Fatal("it must ask first")
			}
			said := plain(question.content())
			for _, phrase := range want.said {
				if !strings.Contains(said, phrase) {
					t.Errorf("the question must say %q:\n%s", phrase, said)
				}
			}
			if want.warns != strings.Contains(said, "run it again") {
				t.Errorf("running the statement again is worth a tick box:\n%s", said)
			}
			left, _ := press(t, question, "esc")
			if left.modal != nil || left.exporter == nil {
				t.Error("saying no must go back to the form rather than close it")
			}
		})
	}
}

// A form that is not filled in is refused before the question is even asked.
func TestAnExportWithNoFileNameNeverAsks(t *testing.T) {
	dialog, _ := exporting(t, ranSomething(t), "csv")
	fields := append([]field(nil), dialog.exporter.form.fields...)
	for i := range fields {
		if fields[i].key == "path" {
			fields[i].required = true
			fields[i].input.SetValue("")
		}
	}
	dialog.exporter.form.fields = fields
	refused, _ := dialog.confirmExport()
	if refused.(Model).modal != nil {
		t.Error("there is nothing to ask about yet")
	}
	if refused.(Model).exporter.trouble == "" {
		t.Error("and the form must say what is missing")
	}
}

// Saying yes to the question writes the file.
func TestSayingYesWritesTheFile(t *testing.T) {
	dialog, path := exporting(t, ranSomething(t), "csv")
	set4Export(dialog.exporter, "scope", scopeOnScreen)
	asked, _ := dialog.confirmExport()
	answered, cmd := press(t, asked.(Model), "enter")
	if cmd == nil {
		t.Fatal("enter must answer it")
	}
	started := answered
	for _, msg := range runAll(t, cmd) {
		updated, next := started.Update(msg)
		started = settle(t, updated.(Model), next)
	}
	done := started
	if done.exporter != nil {
		t.Fatalf("the dialog must close: %+v", done.exporter.trouble)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("the file must be there: %v", err)
	}
}
