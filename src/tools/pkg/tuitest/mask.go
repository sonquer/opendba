package tuitest

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// Apply replaces everything the masks match, so that a screen which cannot be
// drawn the same way twice can still be compared with the last time.
//
// A replacement is padded to the width of what it replaced, because in a
// terminal the columns a value lines up with are part of what is being
// compared, and a mask that moved them would hide the very drift it is there to
// let through.
func (s Suite) Apply(text string) string {
	for _, mask := range s.Masks {
		if mask.compiled == nil {
			continue
		}
		text = mask.compiled.ReplaceAllStringFunc(text, func(match string) string {
			return fit(mask.With, ansi.StringWidth(match))
		})
	}
	return trimRight(text)
}

func trimRight(text string) string {
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		lines[i] = strings.TrimRight(line, " \t")
	}
	return strings.Join(lines, "\n")
}

func fit(replacement string, width int) string {
	short := width - ansi.StringWidth(replacement)
	if short <= 0 {
		return replacement
	}
	return replacement + strings.Repeat(" ", short)
}

// Leaked is the list of strings that must never be drawn and were.
func (s Suite) Leaked(text string) []string {
	var found []string
	for _, secret := range s.Forbid {
		if secret != "" && strings.Contains(text, secret) {
			found = append(found, secret)
		}
	}
	return found
}
