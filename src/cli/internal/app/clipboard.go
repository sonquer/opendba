package app

import (
	"bytes"
	"fmt"

	tea "charm.land/bubbletea/v2"

	"github.com/sonquer/tui4db/src/cli/internal/export"
	"github.com/sonquer/tui4db/src/cli/internal/ui"
)

// copyMsg asks for part of the result to be put on the clipboard.
type copyMsg struct {
	whole  bool
	format export.Format
}

// copied puts rows on the clipboard, written by the same code that writes them
// to a file so the two cannot disagree about what a value looks like.
func (m Model) copied(msg copyMsg) (tea.Model, tea.Cmd) {
	if !m.results.present || m.results.failure != "" || len(m.results.rows) == 0 {
		return m, m.notify("there is nothing to copy yet")
	}
	rows := m.results.rows
	said := ui.Plural(len(rows), "row", "rows") + " are"
	if !msg.whole {
		at := min(max(m.results.cursor, 0), len(rows)-1)
		rows, said = rows[at:at+1], "the row is"
	}
	text, err := text4Clipboard(m.results.columns, rows, msg.format)
	if err != nil {
		return m, m.notify("that could not be written: " + err.Error())
	}
	return m, tea.Batch(tea.SetClipboard(text),
		m.notify(fmt.Sprintf("%s on the clipboard, as %s", said, msg.format)))
}

// copiedCell puts the one value under the cursor on the clipboard, which is
// what somebody reaching for a mouse to select an id actually wants.
func (m Model) copiedCell() (tea.Model, tea.Cmd) {
	value, ok := m.results.cell()
	if !ok {
		return m, m.notify("there is nothing to copy yet")
	}
	return m, tea.Batch(tea.SetClipboard(value), m.notify("the value is on the clipboard"))
}

func text4Clipboard(columns []string, rows [][]any, format export.Format) (string, error) {
	var held bytes.Buffer
	writer, err := export.New(format, &held, export.Options{})
	if err != nil {
		return "", err
	}
	if err := writer.Head(columns); err != nil {
		return "", err
	}
	for _, row := range rows {
		if err := writer.Row(row); err != nil {
			return "", err
		}
	}
	if err := writer.Close(); err != nil {
		return "", err
	}
	return held.String(), nil
}
