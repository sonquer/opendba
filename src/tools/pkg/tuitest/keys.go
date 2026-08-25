// Package tuitest drives a terminal program through a pseudo-terminal and
// asserts what it draws.
package tuitest

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// Modifier is the set of modifier keys held down with a key.
type Modifier int

// The modifier bits, in the order xterm numbers them.
const (
	ModShift Modifier = 1 << iota
	ModAlt
	ModCtrl
)

// parameter is how xterm names this modifier set in an escape sequence.
func (m Modifier) parameter() int { return int(m) + 1 }

type named struct {
	sequence string
	tilde    int
	letter   byte
}

var namedKeys = map[string]named{
	"enter":     {sequence: "\r"},
	"return":    {sequence: "\r"},
	"tab":       {sequence: "\t"},
	"esc":       {sequence: "\x1b"},
	"escape":    {sequence: "\x1b"},
	"space":     {sequence: " "},
	"backspace": {sequence: "\x7f"},
	"up":        {letter: 'A'},
	"down":      {letter: 'B'},
	"right":     {letter: 'C'},
	"left":      {letter: 'D'},
	"end":       {letter: 'F'},
	"home":      {letter: 'H'},
	"insert":    {tilde: 2},
	"delete":    {tilde: 3},
	"pgup":      {tilde: 5},
	"pgdown":    {tilde: 6},
	"f1":        {sequence: "\x1bOP"},
	"f2":        {sequence: "\x1bOQ"},
	"f3":        {sequence: "\x1bOR"},
	"f4":        {sequence: "\x1bOS"},
	"f5":        {tilde: 15},
	"f6":        {tilde: 17},
	"f7":        {tilde: 18},
	"f8":        {tilde: 19},
	"f9":        {tilde: 20},
	"f10":       {tilde: 21},
	"f11":       {tilde: 23},
	"f12":       {tilde: 24},
}

var modifiers = map[string]Modifier{
	"ctrl":    ModCtrl,
	"control": ModCtrl,
	"alt":     ModAlt,
	"option":  ModAlt,
	"meta":    ModAlt,
	"shift":   ModShift,
}

// Encode turns a key name such as "ctrl+r", "f5" or "shift+tab" into the bytes
// a terminal sends when that key is pressed.
func Encode(name string) ([]byte, error) {
	if name == "" {
		return nil, fmt.Errorf("encode a key: the name is empty")
	}
	parts := strings.Split(name, "+")
	base := parts[len(parts)-1]
	var mod Modifier
	for _, part := range parts[:len(parts)-1] {
		bit, ok := modifiers[strings.ToLower(part)]
		if !ok {
			return nil, fmt.Errorf("encode %q: %q is not a modifier", name, part)
		}
		mod |= bit
	}
	if key, ok := namedKeys[strings.ToLower(base)]; ok {
		return encodeNamed(strings.ToLower(base), key, mod), nil
	}
	if utf8.RuneCountInString(base) != 1 {
		return nil, fmt.Errorf("encode %q: %q is not a key", name, base)
	}
	letter, _ := utf8.DecodeRuneInString(base)
	return encodeRune(letter, mod), nil
}

func encodeNamed(name string, key named, mod Modifier) []byte {
	if mod == 0 {
		if key.sequence != "" {
			return []byte(key.sequence)
		}
		if key.letter != 0 {
			return []byte(fmt.Sprintf("\x1b[%c", key.letter))
		}
		return []byte(fmt.Sprintf("\x1b[%d~", key.tilde))
	}
	if name == "tab" && mod == ModShift {
		return []byte("\x1b[Z")
	}
	if key.letter != 0 {
		return []byte(fmt.Sprintf("\x1b[1;%d%c", mod.parameter(), key.letter))
	}
	if key.tilde != 0 {
		return []byte(fmt.Sprintf("\x1b[%d;%d~", key.tilde, mod.parameter()))
	}
	return otherKey(rune(key.sequence[0]), mod)
}

func encodeRune(letter rune, mod Modifier) []byte {
	switch mod {
	case 0:
		return []byte(string(letter))
	case ModAlt:
		return append([]byte("\x1b"), []byte(string(letter))...)
	case ModCtrl:
		if control, ok := controlByte(letter); ok {
			return []byte{control}
		}
	case ModCtrl | ModAlt:
		if control, ok := controlByte(letter); ok {
			return []byte{0x1b, control}
		}
	}
	return otherKey(letter, mod)
}

func controlByte(letter rune) (byte, bool) {
	lower := letter
	if lower >= 'A' && lower <= 'Z' {
		lower += 'a' - 'A'
	}
	switch {
	case lower >= 'a' && lower <= 'z':
		return byte(lower-'a') + 1, true
	case lower == ' ' || lower == '@':
		return 0, true
	case lower >= '[' && lower <= '_':
		return byte(lower-'[') + 0x1b, true
	case lower == '?':
		return 0x7f, true
	}
	return 0, false
}

// otherKey encodes a combination no legacy sequence covers, using the
// modifyOtherKeys form the program turns on when it starts.
func otherKey(letter rune, mod Modifier) []byte {
	return []byte(fmt.Sprintf("\x1b[27;%d;%d~", mod.parameter(), letter))
}

// EncodeAll joins the bytes for a list of key names.
func EncodeAll(names []string) ([]byte, error) {
	var out []byte
	for _, name := range names {
		encoded, err := Encode(name)
		if err != nil {
			return nil, err
		}
		out = append(out, encoded...)
	}
	return out, nil
}
