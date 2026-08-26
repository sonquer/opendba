package tuitest

import "testing"

func TestEncodeNamesTheBytesATerminalSends(t *testing.T) {
	cases := map[string]struct {
		name string
		want string
	}{
		"a plain letter is itself":            {"e", "e"},
		"enter is a carriage return":          {"enter", "\r"},
		"escape is one byte":                  {"esc", "\x1b"},
		"space is a space":                    {"space", " "},
		"backspace is delete":                 {"backspace", "\x7f"},
		"an arrow is a cursor sequence":       {"up", "\x1b[A"},
		"a page key is a tilde sequence":      {"pgdown", "\x1b[6~"},
		"an early function key is SS3":        {"f4", "\x1bOS"},
		"a later function key is a tilde":     {"f5", "\x1b[15~"},
		"control folds a letter to its byte":  {"ctrl+r", "\x12"},
		"control of a is one":                 {"ctrl+a", "\x01"},
		"control is case insensitive":         {"ctrl+R", "\x12"},
		"alt prefixes with escape":            {"alt+x", "\x1bx"},
		"shift and tab have their own":        {"shift+tab", "\x1b[Z"},
		"a modified arrow carries the number": {"ctrl+up", "\x1b[1;5A"},
		"a modified page key too":             {"ctrl+pgup", "\x1b[5;5~"},
		"control and alt stack":               {"ctrl+alt+a", "\x1b\x01"},
		"a digit under control has no legacy": {"ctrl+1", "\x1b[27;5;49~"},
		"so does enter under control":         {"ctrl+enter", "\x1b[27;5;13~"},
	}
	for name, test := range cases {
		t.Run(name, func(t *testing.T) {
			got, err := Encode(test.name)
			if err != nil {
				t.Fatalf("Encode(%q) = %v", test.name, err)
			}
			if string(got) != test.want {
				t.Errorf("Encode(%q) = %q, want %q", test.name, got, test.want)
			}
		})
	}
}

func TestEncodeRefusesWhatIsNotAKey(t *testing.T) {
	for _, name := range []string{"", "hyper+a", "ctrl+nope", "wingding"} {
		if _, err := Encode(name); err == nil {
			t.Errorf("Encode(%q) was accepted", name)
		}
	}
}

func TestEncodeAllJoinsEveryKey(t *testing.T) {
	got, err := EncodeAll([]string{"ctrl+p", "down", "enter"})
	if err != nil {
		t.Fatalf("EncodeAll = %v", err)
	}
	if string(got) != "\x10\x1b[B\r" {
		t.Errorf("EncodeAll = %q", got)
	}
	if _, err := EncodeAll([]string{"down", "nope+x"}); err == nil {
		t.Error("EncodeAll accepted a key that does not exist")
	}
}
