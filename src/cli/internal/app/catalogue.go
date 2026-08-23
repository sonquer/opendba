package app

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/sonquer/tui4db/src/cli/internal/driver"
	"github.com/sonquer/tui4db/src/cli/internal/ui"
)

// listingPage opens the row under the cursor on whichever of the two catalogue
// lists is on screen.
func (m Model) listingPage() (tea.Model, tea.Cmd) {
	if m.view == viewIndexes {
		shown := m.shownIndexes()
		if m.listing < 0 || m.listing >= len(shown) {
			return m, nil
		}
		page := m.indexPage(shown[m.listing])
		page.ask = m.build != nil
		m.page = &page
		return m, nil
	}
	shown := m.shownTables()
	if m.listing < 0 || m.listing >= len(shown) {
		return m, nil
	}
	table := shown[m.listing]
	page := m.tablePage(table)
	page.ask = m.build != nil
	m.page = &page
	return m, m.readOneColumn(table.Name)
}

// repage rebuilds an open table page once its columns arrive, because a page
// asked for them and a page that never shows them is the wrong answer.
func (m Model) repage(table string) Model {
	if m.page == nil {
		return m
	}
	for _, known := range m.tables {
		if known.Name != table || known.Qualified() != m.page.title {
			continue
		}
		rebuilt := m.tablePage(known)
		rebuilt.offset = m.page.offset
		m.page = &rebuilt
	}
	return m
}

// tablePage is what a table is and how it is being used: its columns, the
// numbers the server keeps about it, and what those numbers mean for someone
// who has to decide whether to do anything about them.
func (m Model) tablePage(table driver.Table) details {
	page := details{
		theme: m.theme,
		title: table.Qualified(),
		tag:   m.theme.Severity(m.theme.Severity4Driver(table.Health())).Render(table.Kind),
		pairs: tableFacts(table),
	}
	if table.Stats && table.CacheHit > 0 {
		reading := ui.Reading{
			Label:    "in memory",
			Value:    fmt.Sprintf("%.1f%%", table.CacheHit*100),
			Ratio:    table.CacheHit,
			Measured: true,
			Severity: m.theme.Severity4Driver(table.Health()),
		}
		page.meter = &reading
	}
	for _, column := range m.fields[table.Name] {
		page.pairs = append(page.pairs, pair{key: column.Name, value: describe(column)})
	}
	page.prose = tableProse(table) + "\n\n" + m.indexesOf(table.Name)
	return page
}

func tableFacts(table driver.Table) []pair {
	facts := []pair{
		{key: "rows", value: ui.Count(table.Rows)},
		{key: "size", value: driver.ByteSize(table.Size)},
	}
	if table.IndexSize > 0 {
		facts = append(facts, pair{key: "indexes", value: driver.ByteSize(table.IndexSize)})
	}
	if table.Comment != "" {
		facts = append(facts, pair{key: "comment", value: table.Comment})
	}
	if !table.Stats {
		return facts
	}
	facts = append(facts,
		pair{key: "read by index", value: ui.Count(table.IndexScans)},
		pair{key: "read end to end", value: ui.Count(table.SeqScans)},
		pair{key: "dead rows", value: ui.Count(table.DeadRows)})
	if !table.LastVacuum.IsZero() {
		facts = append(facts, pair{key: "vacuumed", value: ago(table.LastVacuum)})
	}
	return facts
}

// tableProse says the same thing twice: once for whoever has to decide what to
// do, and once in the counters a specialist will want to check it against.
func tableProse(table driver.Table) string {
	if !table.Stats {
		return "## How it is read\n\n" +
			"This driver keeps no counters about how a table is used, so there is " +
			"nothing here to read beyond its size and how many rows it holds."
	}
	parts := []string{"## How it is read", "", reads(table)}
	if dead, ok := table.Dead(); ok {
		parts = append(parts, "", "## What is in it", "", rot(table, dead))
	}
	if table.CacheHit > 0 {
		parts = append(parts, "", "## Where it is read from", "", memory(table))
	}
	return strings.Join(parts, "\n")
}

func reads(table driver.Table) string {
	share, ok := table.Indexed()
	if !ok {
		return "Nothing has read this table since the server last reset its counters."
	}
	counts := fmt.Sprintf("\n\n`%s through an index · %s end to end`",
		ui.Count(table.IndexScans), ui.Count(table.SeqScans))
	switch {
	case share >= 0.95:
		return "Almost every read finds its rows through an index rather than walking " +
			"the table from end to end. That is what an index is for." + counts
	case share >= 0.5:
		return fmt.Sprintf("%.0f%% of reads go through an index and the rest walk the whole "+
			"table. On a small table that is the planner being right: reading all of it is "+
			"cheaper than looking anything up.", share*100) + counts
	default:
		return fmt.Sprintf("%.0f%% of reads walk the whole table from end to end. On a table "+
			"this size that costs real time, and it usually means a query is filtering on "+
			"something no index covers.", (1-share)*100) + counts
	}
}

func rot(table driver.Table, dead float64) string {
	body := fmt.Sprintf("%s rows are live and %s are dead: rows that were changed or "+
		"deleted, still taking room on disk, and read past by everything until a vacuum "+
		"clears them.", ui.Count(table.LiveRows), ui.Count(table.DeadRows))
	switch {
	case dead >= 0.20:
		body += fmt.Sprintf(" At %.0f%% dead, one read in five is spent on rows that no "+
			"longer exist. Something is keeping vacuum from finishing: usually a long "+
			"transaction or a replication slot holding the oldest row visible.", dead*100)
	case dead >= 0.10:
		body += fmt.Sprintf(" %.0f%% dead is more than a table this busy should carry, and "+
			"worth watching rather than acting on.", dead*100)
	default:
		body += fmt.Sprintf(" %.0f%% dead is normal for a table that is written to.", dead*100)
	}
	if table.LastVacuum.IsZero() {
		return body + "\n\nNothing has vacuumed this table yet."
	}
	return body + "\n\nLast vacuumed " + ago(table.LastVacuum) + "."
}

func memory(table driver.Table) string {
	if table.CacheHit >= 0.99 {
		return fmt.Sprintf("%.1f%% of the blocks read from this table were already in memory. "+
			"A read the server has to fetch from disk is thousands of times slower than one "+
			"it already has.", table.CacheHit*100)
	}
	return fmt.Sprintf("%.1f%% of the blocks read from this table were already in memory, so "+
		"the rest came from disk. Either the table is bigger than the memory the server was "+
		"given for caching, or it is read rarely enough that it keeps falling out.",
		table.CacheHit*100)
}

// indexPage is what one index costs and what it is worth.
func (m Model) indexPage(index driver.Index) details {
	page := details{
		theme: m.theme,
		title: index.Qualified(),
		tag:   m.theme.Severity(m.theme.Severity4Driver(index.Health())).Render(kindOf(index)),
		pairs: []pair{
			{key: "on", value: index.Table},
			{key: "size", value: driver.ByteSize(index.Size)},
			{key: "reads", value: ui.Count(index.Scans)},
			{key: "rows returned", value: ui.Count(index.Rows)},
		},
		prose: indexProse(index),
		code:  index.Definition,
	}
	return page
}

func kindOf(index driver.Index) string {
	switch {
	case index.Primary:
		return "primary key"
	case index.Unique:
		return "unique"
	default:
		return "index"
	}
}

func indexProse(index driver.Index) string {
	if !index.Stats {
		return "## What it is for\n\nThis driver keeps no counters about index use, so " +
			"there is no way to tell from here whether anything reads this one."
	}
	body := []string{"## What it is for", ""}
	switch {
	case index.Primary:
		body = append(body, "This is the primary key. It is how every row of the table is "+
			"identified, and it refuses two rows with the same one, so it earns its place "+
			"whether or not anything reads it.")
	case index.Idle():
		body = append(body, fmt.Sprintf("Nothing has read this index since the server last "+
			"reset its counters. It still costs %s on disk, and every insert, update and "+
			"delete on %s has to keep it current. An index nobody reads is a tax on every "+
			"write for nothing in return.", driver.ByteSize(index.Size), index.Table))
	case index.Unique:
		body = append(body, fmt.Sprintf("This index refuses duplicates, so it is doing work "+
			"even when nothing reads it. The server has read it %s.",
			ui.Plural(int(index.Scans), "time", "times")))
	default:
		body = append(body, fmt.Sprintf("The server has read this index %s and handed back "+
			"%s from it, which is what an index is for: finding the rows a query wants "+
			"without walking the whole table.",
			ui.Plural(int(index.Scans), "time", "times"), ui.Count(index.Rows)))
	}
	return strings.Join(body, "\n")
}

// ago writes how long ago something happened, at the precision a person would
// say it out loud.
func ago(when time.Time) string {
	since := time.Since(when)
	switch {
	case since < time.Minute:
		return "just now"
	case since < time.Hour:
		return ui.Plural(int(since.Minutes()), "minute", "minutes") + " ago"
	case since < 48*time.Hour:
		return ui.Plural(int(since.Hours()), "hour", "hours") + " ago"
	default:
		return ui.Plural(int(since.Hours()/24), "day", "days") + " ago"
	}
}
