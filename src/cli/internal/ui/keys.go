package ui

import (
	"runtime"
	"strings"
)

var macModifiers = map[string]string{
	"ctrl":  "⌃",
	"alt":   "⌥",
	"opt":   "⌥",
	"shift": "⇧",
	"super": "⌘",
	"cmd":   "⌘",
}

// macKeys are the glyphs a Mac keyboard prints, minus the ones most terminal
// fonts draw as something else entirely. Those keep their name.
var macKeys = map[string]string{
	"enter":     "enter",
	"esc":       "esc",
	"tab":       "tab",
	"space":     "space",
	"backspace": "backspace",
	"delete":    "delete",
	"up":        "↑",
	"down":      "↓",
	"left":      "←",
	"right":     "→",
	"pgup":      "⇞",
	"pgdown":    "⇟",
	"home":      "↖",
	"end":       "↘",
}

var plainModifiers = map[string]string{
	"ctrl":  "ctrl+",
	"alt":   "alt+",
	"opt":   "alt+",
	"shift": "shift+",
	"super": "win+",
	"cmd":   "win+",
}

// Keystroke renders a Bubble Tea key name the way the platform writes it:
// ⌃⏎ on macOS, ctrl+enter elsewhere.
func Keystroke(key string) string {
	parts := strings.Split(key, "+")
	last := parts[len(parts)-1]
	modifiers := parts[:len(parts)-1]

	if runtime.GOOS != "darwin" {
		out := ""
		for _, modifier := range modifiers {
			out += plainModifiers[modifier]
		}
		return out + last
	}

	out := ""
	for _, modifier := range modifiers {
		out += macModifiers[modifier]
	}
	if glyph, ok := macKeys[last]; ok {
		return out + glyph
	}
	if len(modifiers) > 0 {
		return out + strings.ToUpper(last)
	}
	return last
}

// Keystrokes renders several key names as one hint, such as ⌃⏎ or ⌘⏎.
func Keystrokes(keys ...string) string {
	rendered := make([]string, 0, len(keys))
	for _, key := range keys {
		rendered = append(rendered, Keystroke(key))
	}
	return strings.Join(rendered, " or ")
}
