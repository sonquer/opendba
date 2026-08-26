package tuitest

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// Frame is one look at the emulated screen.
type Frame struct {
	Width  int
	Height int
	Alt    bool
	Styled string
}

// Plain is the frame with the styling removed, every line trimmed on the right
// and the empty lines at the bottom dropped, so that a golden file holds what
// was drawn rather than the padding around it.
func (f Frame) Plain() string {
	lines := strings.Split(ansi.Strip(f.Styled), "\n")
	for i, line := range lines {
		lines[i] = strings.TrimRight(line, " \t\r")
	}
	for len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return strings.Join(lines, "\n")
}

// Contains reports whether the text appears on the screen, ignoring styling.
func (f Frame) Contains(text string) bool {
	return strings.Contains(ansi.Strip(f.Styled), text)
}

// Lines is the frame as trimmed lines.
func (f Frame) Lines() []string {
	plain := f.Plain()
	if plain == "" {
		return nil
	}
	return strings.Split(plain, "\n")
}
