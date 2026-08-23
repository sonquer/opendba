package local

import "unicode/utf8"

// Pieces reassembles what a model emits into text. A token is a run of bytes
// rather than a character, so one rune can arrive split across two of them, and
// printing each piece as it lands would put a replacement character on screen
// in the middle of a word.
type Pieces struct {
	pending []byte
}

// Add returns the text that is now complete, holding back any trailing bytes
// that begin a rune whose remainder has not arrived.
func (p *Pieces) Add(chunk []byte) string {
	p.pending = append(p.pending, chunk...)
	cut := 0
	for cut < len(p.pending) {
		if !utf8.FullRune(p.pending[cut:]) {
			break
		}
		_, size := utf8.DecodeRune(p.pending[cut:])
		cut += size
	}
	text := string(p.pending[:cut])
	p.pending = append(p.pending[:0], p.pending[cut:]...)
	return text
}

// Flush returns whatever is left, however broken. The stream has ended, and
// holding the bytes back any longer would only lose them.
func (p *Pieces) Flush() string {
	text := string(p.pending)
	p.pending = p.pending[:0]
	return text
}
