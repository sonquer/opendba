package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/sonquer/tui4db/src/cli/internal/cli"
	"github.com/sonquer/tui4db/src/cli/internal/driver"
	"github.com/sonquer/tui4db/src/cli/internal/export"
	"github.com/sonquer/tui4db/src/cli/internal/ui"
)

type exportMsg struct{}

type exportProgressMsg struct {
	rows  int
	token int
}

type exportEndedMsg struct {
	rows  int
	path  string
	err   error
	token int
}

// running4Export is the two channels an export in flight is followed by: how
// far it has got, and how it ended.
type running4Export struct {
	progress chan int
	done     chan finished4Export
}

// finished4Export is how an export ended: how many rows reached the file, and
// what stopped it if anything did. The count travels with the outcome rather
// than as a last word on the progress channel, because a last word nobody is
// left to hear is a goroutine that never returns.
type finished4Export struct {
	rows int
	err  error
}

// exporter is the question a file is written from: what to write, where, and
// how much of the result.
//
// The scope matters more than it looks. The rows on screen are capped at the
// profile's row limit and have been folded onto one line and cut to the width
// of a column; everything is the statement run again with the cap lifted. The
// second is the default, and is the only one that gives the file what the
// server would give it.
type exporter struct {
	theme     *ui.Theme
	form      form
	statement string
	columns   []string
	rows      [][]any
	reads     bool
	refusal   string
	trouble   string
	rowsSeen  int
	busy      bool
	token     int
	running   running4Export
}

const (
	scopeEverything = "everything"
	scopeOnScreen   = "what is on screen"
)

// export4Result raises the question. It refuses before it asks when there is no
// result to write, because a dialog about nothing is worse than a sentence
// saying so.
func (m Model) export4Result() (tea.Model, tea.Cmd) {
	if !m.results.present || m.results.failure != "" {
		return m, m.notify("there is no result to export yet")
	}
	verdict := m.session.Guard.Classify(m.results.statement,
		cli.Mode(m.session.Connection.Mode))
	dialog := &exporter{
		theme:     m.theme,
		statement: m.results.statement,
		columns:   m.results.columns,
		rows:      m.results.rows,
		reads:     verdict.Allowed(),
	}
	if !dialog.reads {
		dialog.refusal = "this statement changes data, so it is not run a second time; " +
			"what is on screen is what will be written"
	}
	fields := []field{
		choiceField("format", "format", formatNames(), string(export.FormatCSV),
			"the file this result is written as"),
		textField(m.theme, "path", "file", m.suggestedPath(export.FormatCSV),
			"where to write it"),
		choiceField("scope", "rows", dialog.scopes(), dialog.scopes()[0],
			"everything runs the statement again with no row cap"),
		actionField("write", "write the file", "enter writes it"),
	}
	built, cmd := newForm(fields...)
	dialog.form = built
	m.exporter = dialog
	return m, cmd
}

// scopes is what may be written. A statement that cannot be run again offers
// one answer rather than an answer that would be quietly overruled.
func (e exporter) scopes() []string {
	if !e.reads {
		return []string{scopeOnScreen}
	}
	return []string{scopeEverything, scopeOnScreen}
}

func formatNames() []string {
	names := make([]string, 0, len(export.Formats()))
	for _, format := range export.Formats() {
		names = append(names, string(format))
	}
	return names
}

// suggestedPath is a name nobody has to think about: the connection, the tab,
// and the moment, in the directory the person started the program from.
func (m Model) suggestedPath(format export.Format) string {
	parts := []string{m.session.Connection.Name, m.label(m.worksheet, m.sheet)}
	name := strings.Join(parts, "-") + "-" + time.Now().Format("20060102-150405") +
		"." + format.Extension()
	return filepath.Join(".", clean4Path(name))
}

// clean4Path takes out what a file name cannot hold, so a tab called
// catalog.product_prices does not become a directory nobody meant.
func clean4Path(name string) string {
	cleaned := strings.Map(func(r rune) rune {
		if strings.ContainsRune(`/\:*?"<>|`, r) || r < ' ' {
			return '-'
		}
		if r == ' ' {
			return '-'
		}
		return r
	}, name)
	return strings.Trim(cleaned, "-")
}

func (m Model) exportKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	dialog := *m.exporter
	if dialog.busy {
		if key4Back(msg) {
			m.exporter = nil
			return m.stopExporting(), nil
		}
		return m, nil
	}
	if key4Back(msg) {
		m.exporter = nil
		return m, nil
	}
	before := dialog.form.value("format")
	updated, action, cmd := dialog.form.update(msg)
	dialog.form = updated
	if after := updated.value("format"); after != before {
		dialog.form = dialog.renamed(after)
	}
	if action == "write" {
		m.exporter = &dialog
		return m.confirmExport()
	}
	m.exporter = &dialog
	return m, cmd
}

// renamed follows the format with the file name, so choosing xlsx after typing
// nothing does not write a spreadsheet called something.csv. A name that was
// typed by hand is left alone.
func (e exporter) renamed(name string) form {
	format, ok := export.Named(name)
	if !ok {
		return e.form
	}
	path := e.form.value("path")
	suffix := filepath.Ext(path)
	if suffix == "" {
		return e.form
	}
	fields := append([]field(nil), e.form.fields...)
	for i := range fields {
		if fields[i].key != "path" {
			continue
		}
		fields[i].input.SetValue(strings.TrimSuffix(path, suffix) + "." + format.Extension())
	}
	e.form.fields = fields
	return e.form
}

func key4Back(msg tea.KeyPressMsg) bool { return msg.String() == "esc" }

type writeExportMsg struct{}

// confirmExport says what is about to be written before it is written. An
// export is the one thing this program does that reaches outside it: it puts a
// file on the disk, and when the scope is everything it sends the statement to
// the server a second time with no row cap and no timeout. Both are worth one
// sentence and one key.
func (m Model) confirmExport() (tea.Model, tea.Cmd) {
	dialog := *m.exporter
	if err := dialog.form.validate(); err != nil {
		dialog.trouble = err.Error()
		m.exporter = &dialog
		return m, nil
	}
	scope := dialog.form.value("scope")
	format := dialog.form.value("format")
	path := dialog.form.value("path")
	question := ask(m.theme, "write this file?", said4Export(scope, format, dialog),
		writeExportMsg{})
	question.tag = m.theme.Muted.Render(path)
	question.code = dialog.statement
	if scope == scopeEverything {
		question.warning("the statement runs again, with no row cap and no timeout, "+
			"so a large table is a large file and a long wait", "run it again")
	}
	m.modal = question
	return m, nil
}

// said4Export is the sentence the question turns on: how many rows are going
// where, in what.
func said4Export(scope, format string, dialog exporter) string {
	if scope == scopeOnScreen {
		return "the " + ui.Plural(len(dialog.rows), "row", "rows") +
			" already read are written as " + format +
			", exactly as the server gave them rather than as the table drew them"
	}
	return "every row the statement returns is written as " + format +
		", however many that is; the result on screen stopped at the row limit and this will not"
}

// startExport writes the file. Nothing is written where something already is:
// an export that silently replaced yesterday's would be a way to lose data with
// a program that is otherwise not allowed to.
func (m Model) startExport() (tea.Model, tea.Cmd) {
	dialog := *m.exporter
	if err := dialog.form.validate(); err != nil {
		dialog.trouble = err.Error()
		m.exporter = &dialog
		return m, nil
	}
	format, ok := export.Named(dialog.form.value("format"))
	if !ok {
		dialog.trouble = "there is no such format"
		m.exporter = &dialog
		return m, nil
	}
	path := dialog.form.value("path")
	whole := dialog.form.value("scope") == scopeEverything

	ctx, stop := context.WithCancel(context.Background())
	progress := make(chan int)
	done := make(chan finished4Export, 1)
	report := crash{paths: m.workspace.Setup().Store.Paths, version: m.session.Version}
	conn := m.session.Conn
	statement := dialog.statement
	columns, rows := dialog.columns, dialog.rows
	options := export.Options{
		Sheet:   m.label(m.worksheet, m.sheet),
		TempDir: m.workspace.Setup().Store.Paths.State,
	}
	go func() {
		defer close(progress)
		defer func() {
			if cause := recover(); cause != nil {
				done <- finished4Export{err: fell("writing the file", cause,
					report.wrote("writing the file", cause, debug.Stack()))}
			}
		}()
		written, err := write4Export(ctx, job4Export{
			conn:      conn,
			statement: statement,
			columns:   columns,
			rows:      rows,
			whole:     whole,
			format:    format,
			path:      path,
			options:   options,
			progress:  progress,
		})
		done <- finished4Export{rows: written, err: err}
	}()

	dialog.token++
	dialog.busy = true
	dialog.trouble = ""
	dialog.rowsSeen = 0
	dialog.running = running4Export{progress: progress, done: done}
	m.exporter = &dialog
	m.stopExport = stop
	return m, tea.Batch(watchExport(dialog.running, dialog.token, path), m.spinner.Tick)
}

// job4Export is everything writing one file needs, gathered so the goroutine
// that does it closes over one value rather than over the model.
type job4Export struct {
	conn      driver.Conn
	statement string
	columns   []string
	rows      [][]any
	whole     bool
	format    export.Format
	path      string
	options   export.Options
	progress  chan<- int
}

// tellEvery is how many rows go by between two words about it. Often enough
// that a long export is visibly moving, rarely enough that saying so is not
// most of the work.
const tellEvery = 2000

// write4Export writes the file beside the one it is meant to be and renames it
// when it is whole, which is how everything else this program writes is
// written. An export that was given up on leaves nothing behind.
func write4Export(ctx context.Context, job job4Export) (written int, err error) {
	if _, err := os.Stat(job.path); err == nil {
		return 0, fmt.Errorf("%s is already there", job.path)
	}
	file, err := os.CreateTemp(filepath.Dir(job.path), ".tui4db-export-*")
	if err != nil {
		return 0, fmt.Errorf("make the file: %w", err)
	}
	partial := file.Name()
	defer func() {
		if err != nil {
			_ = file.Close()
			_ = os.Remove(partial)
		}
	}()
	writer, err := export.New(job.format, file, job.options)
	if err != nil {
		return 0, err
	}
	if err := writer.Head(job.columns); err != nil {
		return 0, err
	}
	written, err = job.pour(ctx, writer)
	if err != nil {
		return written, err
	}
	if err := writer.Close(); err != nil {
		return written, err
	}
	if err := file.Close(); err != nil {
		return written, fmt.Errorf("finish the file: %w", err)
	}
	if err := os.Chmod(partial, 0o600); err != nil {
		return written, fmt.Errorf("protect the file: %w", err)
	}
	if err := os.Rename(partial, job.path); err != nil {
		return written, fmt.Errorf("put the file where it belongs: %w", err)
	}
	return written, nil
}

// pour hands every row to the writer, either from the result already in memory
// or from the statement run again with the cap lifted.
func (job job4Export) pour(ctx context.Context, writer export.Writer) (int, error) {
	if !job.whole {
		for i, row := range job.rows {
			if err := writer.Row(row); err != nil {
				return i, err
			}
			job.tell(ctx, i+1)
		}
		return len(job.rows), nil
	}
	result, err := job.conn.Stream(ctx, job.statement)
	if err != nil {
		return 0, err
	}
	defer func() { _ = result.Close() }()
	written := 0
	for result.Next() {
		if err := writer.Row(result.Values()); err != nil {
			return written, err
		}
		written++
		job.tell(ctx, written)
	}
	if err := result.Err(); err != nil {
		return written, fmt.Errorf("read the rows: %w", err)
	}
	return written, nil
}

// tell says how far the file has got, without ever waiting to be heard: an
// export must not stop because nobody is reading the count.
func (job job4Export) tell(ctx context.Context, written int) {
	if written%tellEvery != 0 {
		return
	}
	select {
	case job.progress <- written:
	case <-ctx.Done():
	default:
	}
}

func watchExport(running running4Export, token int, path string) tea.Cmd {
	return func() tea.Msg {
		written, open := <-running.progress
		if !open {
			ended := <-running.done
			return exportEndedMsg{rows: ended.rows, err: ended.err, token: token, path: path}
		}
		return exportProgressMsg{rows: written, token: token}
	}
}

func (m Model) exporting(msg exportProgressMsg) (tea.Model, tea.Cmd) {
	if m.exporter == nil || msg.token != m.exporter.token {
		return m, nil
	}
	dialog := *m.exporter
	dialog.rowsSeen = msg.rows
	m.exporter = &dialog
	return m, watchExport(dialog.running, dialog.token, dialog.form.value("path"))
}

func (m Model) exported(msg exportEndedMsg) (tea.Model, tea.Cmd) {
	if m.exporter == nil || msg.token != m.exporter.token {
		return m, nil
	}
	m.stopExport = nil
	dialog := *m.exporter
	dialog.busy = false
	if msg.err != nil {
		dialog.trouble = msg.err.Error()
		m.exporter = &dialog
		return m, nil
	}
	m.exporter = nil
	return m, m.notify(fmt.Sprintf("wrote %s to %s",
		ui.Plural(msg.rows, "row", "rows"), msg.path))
}

// stopExporting gives up on a file that is being written. What has been written
// so far is thrown away rather than left as a file that looks whole.
func (m Model) stopExporting() Model {
	if m.stopExport != nil {
		m.stopExport()
	}
	m.stopExport = nil
	return m
}

func (e exporter) view(width, height int) string {
	inner := min(ui.TextWidth(width)-6, 72)
	lines := []string{e.theme.Title.Render("export the result"), ""}
	if e.refusal != "" {
		lines = append(lines, e.theme.Severity(ui.SevWarn).Render("⚠ "+wrap(e.refusal, inner-2)), "")
	}
	lines = append(lines, e.form.view(e.theme, inner))
	if e.busy {
		lines = append(lines, "", e.theme.Muted.Render(
			"writing "+ui.Plural(e.rowsSeen, "row", "rows")+" so far"))
	}
	if e.trouble != "" {
		lines = append(lines, "", e.theme.Error.Render("✗ "+wrap(e.trouble, inner-2)))
	}
	hints := []ui.Hint{{Key: "enter", Does: "write"}, {Key: "esc", Does: "cancel"}}
	if e.busy {
		hints = []ui.Hint{{Key: "esc", Does: "stop writing"}}
	}
	lines = append(lines, "", e.theme.Hints(hints...))
	return e.theme.Panel.Render(square(strings.Join(lines, "\n"), inner))
}
