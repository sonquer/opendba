package app

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"

	"github.com/sonquer/opendba/src/cli/internal/ui"
)

// TestBadgesStandInOneColumn is the symmetry: whatever a row is called, the
// badge after it starts in the same place, so the badges read down the list
// rather than following each label wherever it happened to end.
func TestBadgesStandInOneColumn(t *testing.T) {
	theme := ui.Default()
	rows := []row{
		{key: "a", label: "localhost", badge: theme.Mode("READ / WRITE")},
		{key: "b", label: "INSERT2022", badge: theme.Mode(ui.ReadOnlyLabel)},
		{key: "c", label: "a-much-longer-connection", badge: theme.Mode(ui.ReadOnlyLabel)},
	}
	p := picker{theme: theme, rows: rows}
	column := p.column()

	starts := map[int]bool{}
	for i, item := range rows {
		line := p.line(item, 96, i == 0, column)
		at := strings.Index(line, item.badge)
		if at < 0 {
			t.Fatalf("the badge of %q was not drawn", item.label)
		}
		starts[lipgloss.Width(line[:at])] = true
	}
	if len(starts) != 1 {
		t.Errorf("badges start at %d different places, want one: %v", len(starts), starts)
	}
}

// TestEveryModeBadgeIsTheSameSize is the other half of it: a column of badges
// two sizes wide would still read as ragged.
func TestEveryModeBadgeIsTheSameSize(t *testing.T) {
	theme := ui.Default()
	readOnly := lipgloss.Width(theme.Mode(ui.ReadOnlyLabel))
	readWrite := lipgloss.Width(theme.Mode("READ / WRITE"))
	if readOnly != readWrite {
		t.Errorf("badge widths are %d and %d, want them equal", readOnly, readWrite)
	}
}

// TestAListWithNothingAfterItsLabelsKeepsNoColumn stops every other picker in
// the program from growing a margin it has no use for.
func TestAListWithNothingAfterItsLabelsKeepsNoColumn(t *testing.T) {
	p := picker{theme: ui.Default(), rows: []row{{key: "a", label: "one"}, {key: "b", label: "another"}}}
	if p.column() != 0 {
		t.Errorf("column = %d, want none", p.column())
	}
	line := p.line(p.rows[0], 60, false, p.column())
	if strings.Contains(line, "one    ") {
		t.Errorf("a label with nothing after it is not padded: %q", line)
	}
}
