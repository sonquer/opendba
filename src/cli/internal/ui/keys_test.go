package ui

import (
	"runtime"
	"testing"

	"charm.land/lipgloss/v2"
)

func TestKeystroke(t *testing.T) {
	mac := map[string]string{
		"ctrl+enter":  "⌃enter",
		"super+enter": "⌘enter",
		"ctrl+k":      "⌃K",
		"esc":         "esc",
		"tab":         "tab",
		"enter":       "enter",
		"up":          "↑",
		"q":           "q",
		"pgdown":      "⇟",
		"shift+tab":   "⇧tab",
	}
	plainNames := map[string]string{
		"ctrl+enter":  "ctrl+enter",
		"super+enter": "win+enter",
		"ctrl+k":      "ctrl+k",
		"esc":         "esc",
		"q":           "q",
	}
	for key, expected := range mac {
		if got := keystroke(key, true); got != expected {
			t.Errorf("keystroke(%q, mac) = %q, want %q", key, got, expected)
		}
	}
	for key, expected := range plainNames {
		if got := keystroke(key, false); got != expected {
			t.Errorf("keystroke(%q, elsewhere) = %q, want %q", key, got, expected)
		}
	}
	want := mac
	if runtime.GOOS != "darwin" {
		want = plainNames
	}
	for key, expected := range want {
		if got := Keystroke(key); got != expected {
			t.Errorf("Keystroke(%q) = %q, want %q", key, got, expected)
		}
	}
}

// A modifier spelled out is far wider than the glyph a Mac keyboard prints, and
// the widest of them is what a row of keys has to be laid out against.
func TestAKeyIsWiderWhereItIsSpelledOut(t *testing.T) {
	for _, key := range []string{"ctrl+r", "ctrl+pgdown", "shift+tab", "super+enter"} {
		mac := lipgloss.Width(keystroke(key, true))
		spelled := lipgloss.Width(keystroke(key, false))
		if spelled <= mac {
			t.Errorf("%q is %d wide spelled out and %d as glyphs, which cannot be right",
				key, spelled, mac)
		}
	}
}

func TestKeystrokes(t *testing.T) {
	got := Keystrokes("ctrl+r", "f5")
	if got == "" {
		t.Fatal("Keystrokes() rendered nothing")
	}
	if got != Keystroke("ctrl+r")+" or "+Keystroke("f5") {
		t.Errorf("Keystrokes() = %q", got)
	}
}

func TestMacKeyboardFollowsThePlatformUnlessItIsToldNotTo(t *testing.T) {
	if !macKeyboard("mac") {
		t.Error("a run pinned to mac did not write keys the way a Mac does")
	}
	if macKeyboard("plain") {
		t.Error("a run pinned to plain wrote keys the way a Mac does")
	}
	if macKeyboard("") != (runtime.GOOS == "darwin") {
		t.Error("a run that says nothing did not follow the platform")
	}
	if macKeyboard("nonsense") != (runtime.GOOS == "darwin") {
		t.Error("a style nobody offers did not follow the platform")
	}
}
