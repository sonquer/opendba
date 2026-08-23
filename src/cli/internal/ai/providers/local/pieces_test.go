package local

import "testing"

func TestPieces(t *testing.T) {
	cases := map[string]struct {
		chunks []string
		want   []string
		left   string
	}{
		"plain ascii": {
			chunks: []string{"hel", "lo"},
			want:   []string{"hel", "lo"},
		},
		"a rune split in two": {
			chunks: []string{"\xc5", "\xbaden"},
			want:   []string{"", "źden"},
		},
		"a four byte rune arriving one byte at a time": {
			chunks: []string{"\xf0", "\x9f", "\x93", "\x8a"},
			want:   []string{"", "", "", "\U0001f4ca"},
		},
		"text before an unfinished rune is released": {
			chunks: []string{"ok \xc5"},
			want:   []string{"ok "},
			left:   "\xc5",
		},
		"a byte that can never be a rune is passed through rather than held": {
			chunks: []string{"\xff!"},
			want:   []string{"\xff!"},
		},
		"nothing at all": {
			chunks: []string{""},
			want:   []string{""},
		},
	}
	for name, test := range cases {
		t.Run(name, func(t *testing.T) {
			var buffer Pieces
			for i, chunk := range test.chunks {
				if got := buffer.Add([]byte(chunk)); got != test.want[i] {
					t.Fatalf("Add(%q) = %q, want %q", chunk, got, test.want[i])
				}
			}
			if got := buffer.Flush(); got != test.left {
				t.Fatalf("Flush() = %q, want %q", got, test.left)
			}
			if got := buffer.Flush(); got != "" {
				t.Fatalf("Flush() again = %q, want it emptied", got)
			}
		})
	}
}
