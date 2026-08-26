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

// KeyStyle is set at link time, and is empty in a build that ships. It exists so
// that a run which has to draw the same screen on every machine can say how a
// key is written instead of letting the machine decide. The values are "mac"
// and "plain"; anything else follows the platform.
var KeyStyle string

// Keystroke renders a Bubble Tea key name the way the platform writes it:
// ⌃⏎ on macOS, ctrl+enter elsewhere.
func Keystroke(key string) string { return keystroke(key, macKeyboard(KeyStyle)) }

// macKeyboard reports whether keys are written the way a Mac keyboard prints
// them.
func macKeyboard(style string) bool {
	switch style {
	case "mac":
		return true
	case "plain":
		return false
	default:
		return runtime.GOOS == "darwin"
	}
}

// keystroke is Keystroke with the platform as an argument, so that both of the
// ways a key can be written are drawn and measured in a test rather than only
// the way the machine running it happens to write them. The difference is not
// cosmetic: ctrl+r is three times the width of ⌃R, and a row of keys that fits
// on one machine runs off the frame on the other.
func keystroke(key string, mac bool) string {
	parts := strings.Split(key, "+")
	last := parts[len(parts)-1]
	modifiers := parts[:len(parts)-1]

	if !mac {
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
