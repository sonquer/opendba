package tuitest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCompareSaysNothingWhenTheScreenIsTheOneThatWasKept(t *testing.T) {
	path := filepath.Join(t.TempDir(), "screen.txt")
	if err := Write(path, "one\ntwo"); err != nil {
		t.Fatalf("Write = %v", err)
	}
	difference, err := Compare(path, "one\ntwo")
	if err != nil {
		t.Fatalf("Compare = %v", err)
	}
	if difference != "" {
		t.Errorf("Compare() = %q", difference)
	}
}

func TestCompareShowsTheLinesThatMoved(t *testing.T) {
	path := filepath.Join(t.TempDir(), "screen.txt")
	if err := Write(path, "one\ntwo"); err != nil {
		t.Fatalf("Write = %v", err)
	}
	difference, err := Compare(path, "one\nTWO")
	if err != nil {
		t.Fatalf("Compare = %v", err)
	}
	for _, want := range []string{"2 - two", "2 + TWO"} {
		if !strings.Contains(difference, want) {
			t.Errorf("Compare() = %q, want it to mention %q", difference, want)
		}
	}
	if strings.Contains(difference, "one") {
		t.Error("a line that did not move was reported")
	}
}

func TestCompareSaysSoWhenNoScreenHasBeenKept(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing.txt")
	difference, err := Compare(path, "anything")
	if err != nil {
		t.Fatalf("Compare = %v", err)
	}
	if !strings.Contains(difference, "no screen has been kept") {
		t.Errorf("Compare() = %q", difference)
	}
}

func TestCompareFailsWhenTheKeptScreenCannotBeRead(t *testing.T) {
	dir := t.TempDir()
	if _, err := Compare(dir, "anything"); err == nil {
		t.Error("a directory was read as a screen")
	}
}

func TestWriteMakesRoomForTheScreen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "120x36", "one.txt")
	if err := Write(path, "drawn"); err != nil {
		t.Fatalf("Write = %v", err)
	}
	kept, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back = %v", err)
	}
	if string(kept) != "drawn\n" {
		t.Errorf("kept = %q", kept)
	}
}

func TestWriteFailsWhenTheRoomCannotBeMade(t *testing.T) {
	blocked := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(blocked, nil, 0o600); err != nil {
		t.Fatalf("set up = %v", err)
	}
	if err := Write(filepath.Join(blocked, "under", "one.txt"), "drawn"); err == nil {
		t.Error("a screen was written under a file")
	}
}

func TestDifferenceIsEmptyWhenNothingMoved(t *testing.T) {
	if got := Difference("same", "same"); got != "" {
		t.Errorf("Difference() = %q", got)
	}
}

func TestDifferenceCoversTheLinesOneSideDoesNotHave(t *testing.T) {
	got := Difference("one", "one\ntwo")
	if !strings.Contains(got, "2 + two") {
		t.Errorf("Difference() = %q", got)
	}
}
