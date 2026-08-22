package ui

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
)

func TestChromePinsTheFooterToTheLastRow(t *testing.T) {
	screen := plain(Default().Chrome(60, 12, "header", "body", "footer"))
	lines := strings.Split(screen, "\n")
	if len(lines) != 12 {
		t.Fatalf("the screen must fill the window, got %d rows", len(lines))
	}
	if strings.TrimSpace(lines[0]) != "" {
		t.Errorf("the frame breathes at the top: %q", lines[0])
	}
	if !strings.Contains(lines[1], "header") {
		t.Errorf("row 2 = %q", lines[1])
	}
	if !strings.HasPrefix(lines[1], strings.Repeat(" ", 2)) {
		t.Errorf("the frame breathes on the left: %q", lines[1])
	}
	if !strings.Contains(lines[len(lines)-2], "footer") {
		t.Errorf("the footer sits on the last written row = %q", lines[len(lines)-2])
	}
	if !strings.Contains(lines[4], "body") {
		t.Errorf("the body starts under the rule: %q", lines[4])
	}
}

func TestChromeSurvivesATinyWindow(t *testing.T) {
	screen := plain(Default().Chrome(10, 2, "header", "body", "footer"))
	lines := strings.Split(screen, "\n")
	if len(lines) != minChromeHeight {
		t.Fatalf("rows = %d", len(lines))
	}
	if lipgloss.Width(lines[2]) < minChromeWidth {
		t.Errorf("the rule keeps a floor: %q", lines[2])
	}
}

func TestBodyHeightMatchesChrome(t *testing.T) {
	for _, height := range []int{4, 12, 40} {
		body := strings.Repeat("row\n", 100)
		screen := plain(Default().Chrome(40, height, "h", body, "f"))
		rows := len(strings.Split(screen, "\n"))
		if strings.Count(plain(Fit(body, BodyHeight(height))), "row") != BodyHeight(height) {
			t.Errorf("BodyHeight(%d) does not match what Chrome renders", height)
		}
		if rows < minChromeHeight {
			t.Errorf("rows = %d", rows)
		}
	}
}

func TestWidths(t *testing.T) {
	if got := TextWidth(400); got != MaxTextWidth {
		t.Errorf("prose stays readable on a wide terminal, got %d", got)
	}
	if got := TextWidth(10); got != minChromeWidth {
		t.Errorf("a narrow terminal keeps a floor, got %d", got)
	}
	if got := TextWidth(60); got != 56 {
		t.Errorf("TextWidth(60) = %d", got)
	}
	if got := FrameWidth(400); got != 396 {
		t.Errorf("the frame spans the window, got %d", got)
	}
	if got := FrameWidth(10); got != minChromeWidth {
		t.Errorf("FrameWidth(10) = %d", got)
	}
}

func TestFitClipsAndPads(t *testing.T) {
	if got := Fit("a\nb\nc", 2); got != "a\nb" {
		t.Errorf("Fit() = %q", got)
	}
	if got := Fit("a", 3); got != "a\n\n" {
		t.Errorf("Fit() = %q", got)
	}
}

func TestWindowScrolls(t *testing.T) {
	content := "1\n2\n3\n4\n5"
	view, more := Window(content, 0, 2)
	if view != "1\n2" || more != 3 {
		t.Errorf("Window() = %q, %d", view, more)
	}
	view, more = Window(content, 2, 2)
	if view != "3\n4" || more != 1 {
		t.Errorf("Window() = %q, %d", view, more)
	}
	view, more = Window(content, 99, 2)
	if view != "4\n5" || more != 0 {
		t.Errorf("an offset past the end stops at the last row: %q, %d", view, more)
	}
	view, _ = Window(content, -3, 0)
	if view != "1" {
		t.Errorf("Window() = %q", view)
	}
	if got := MaxOffset(content, 2); got != 3 {
		t.Errorf("MaxOffset() = %d", got)
	}
	if got := MaxOffset(content, 99); got != 0 {
		t.Errorf("content that fits does not scroll, got %d", got)
	}
	if got := MaxOffset(content, 0); got != 4 {
		t.Errorf("MaxOffset() = %d", got)
	}
}

func TestOverlayKeepsTheBackground(t *testing.T) {
	background := strings.TrimRight(strings.Repeat("abcdefghij\n", 6), "\n")
	got := plain(Overlay(background, "+--+\n|hi|\n+--+", 10, 6))
	lines := strings.Split(got, "\n")
	if len(lines) != 6 {
		t.Fatalf("rows = %d", len(lines))
	}
	if lines[0] != "abcdefghij" {
		t.Errorf("the background must stay: %q", lines[0])
	}
	if !strings.Contains(got, "|hi|") {
		t.Errorf("the dialog must be drawn: %q", got)
	}
	if !strings.HasPrefix(lines[2], "abc") {
		t.Errorf("the dialog is centred: %q", lines[2])
	}
}

func TestOverlayHandlesADialogBiggerThanTheWindow(t *testing.T) {
	got := plain(Overlay("ab\ncd", strings.Repeat("x", 8), 2, 2))
	if !strings.Contains(got, "xxxxxxxx") {
		t.Errorf("Overlay() = %q", got)
	}
}

func TestCornerDrawsInTheBottomRight(t *testing.T) {
	background := strings.TrimRight(strings.Repeat("abcdefghij\n", 4), "\n")
	lines := strings.Split(plain(Corner(background, "toast", 10, 4)), "\n")
	if len(lines) != 4 {
		t.Fatalf("rows = %d", len(lines))
	}
	if !strings.Contains(lines[0], "toast") {
		t.Errorf("the toast sits on the blank row above the rule: %q", lines[0])
	}
	for _, row := range []int{1, 2, 3} {
		if lines[row] != "abcdefghij" {
			t.Errorf("the rule and the footer stay untouched: %q", lines[row])
		}
	}
}
