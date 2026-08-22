package ui

import (
	"runtime"
	"testing"
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

func TestKeystrokes(t *testing.T) {
	got := Keystrokes("ctrl+r", "f5")
	if got == "" {
		t.Fatal("Keystrokes() rendered nothing")
	}
	if got != Keystroke("ctrl+r")+" or "+Keystroke("f5") {
		t.Errorf("Keystrokes() = %q", got)
	}
}
