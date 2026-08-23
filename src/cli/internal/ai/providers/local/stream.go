package local

import (
	"strings"

	"github.com/sonquer/tui4db/src/cli/internal/ai"
)

// reader turns the flat run of text a local model produces into the bracketed
// chunks the rest of the program reads.
//
// Text is forwarded as it arrives until the model starts a tool call. From that
// point it is held back, because a half written call is not something to show
// anyone and the arguments cannot be read until the closing tag has landed.
type reader struct {
	visible strings.Builder
	held    strings.Builder
	calling bool
	started bool

	// ending is the end of turn marker having been written out as text. It
	// should not happen: the token that ends a turn is a special one, and a
	// sampler that picks it stops the loop before anything is printed. It
	// happens anyway — a grammar that cannot reach the end token, a chat format
	// the file disagrees with, a fine-tune that learned the marker as three
	// ordinary tokens — and what it looks like on screen is the model saying
	// <end_of_turn> to the person, which is worse than any of the causes.
	ending bool
}

// Endings are the markers that mean the model has finished its turn, in every
// form the families this program runs write them. Text from one on is not part
// of the answer.
var Endings = []string{
	"<end_of_turn>",
	"<|im_end|>",
	"<|eot_id|>",
	"<|end_of_text|>",
	"<|endoftext|>",
	"<end_of_utterance>",
	"<|return|>",
	"</s>",
}

// Ended reports whether the model wrote its own end of turn out as text.
func (r *reader) Ended() bool { return r.ending }

// add takes the next piece of text and returns the chunks it produced.
func (r *reader) add(text string) []ai.Chunk {
	if text == "" || r.ending {
		return nil
	}
	if r.calling {
		r.held.WriteString(text)
		return nil
	}
	r.visible.WriteString(text)
	pending := r.visible.String()
	if at := earliest(pending, Endings); at >= 0 {
		r.ending = true
		r.visible.Reset()
		return r.show(pending[:at])
	}
	start := strings.Index(pending, CallOpen)
	if start < 0 {
		return r.show(r.release(pending))
	}
	r.calling = true
	r.visible.Reset()
	r.held.WriteString(pending[start:])
	return r.show(pending[:start])
}

// earliest is where the first of a set of markers begins, or -1 for none.
func earliest(text string, markers []string) int {
	first := -1
	for _, marker := range markers {
		if at := strings.Index(text, marker); at >= 0 && (first < 0 || at < first) {
			first = at
		}
	}
	return first
}

// release keeps back the tail that could still turn out to be the beginning of
// an opening tag, so that the tag is never printed one character at a time
// before it is recognised.
func (r *reader) release(pending string) string {
	keep := partialTag(pending)
	shown := pending[:len(pending)-keep]
	r.visible.Reset()
	r.visible.WriteString(pending[len(pending)-keep:])
	return shown
}

func (r *reader) show(text string) []ai.Chunk {
	if text == "" {
		return nil
	}
	chunks := make([]ai.Chunk, 0, 2)
	if !r.started {
		r.started = true
		chunks = append(chunks, ai.Chunk{Kind: ai.ChunkTextStart})
	}
	return append(chunks, ai.Chunk{Kind: ai.ChunkTextDelta, Text: text})
}

// done closes the answer: whatever text was still held, then the calls the
// model made, then the reason it stopped.
func (r *reader) done(stop ai.StopReason) ([]ai.Chunk, error) {
	chunks := []ai.Chunk{}
	if !r.calling {
		chunks = append(chunks, r.show(r.visible.String())...)
		if r.started {
			chunks = append(chunks, ai.Chunk{Kind: ai.ChunkTextEnd})
		}
		return append(chunks, ai.Chunk{Kind: ai.ChunkDone, Stop: stop}), nil
	}
	if r.started {
		chunks = append(chunks, ai.Chunk{Kind: ai.ChunkTextEnd})
	}
	calls, prose, err := ParseCalls(r.held.String())
	if err != nil {
		return nil, err
	}
	if prose != "" {
		chunks = append(chunks, ai.Chunk{Kind: ai.ChunkTextStart},
			ai.Chunk{Kind: ai.ChunkTextDelta, Text: prose},
			ai.Chunk{Kind: ai.ChunkTextEnd})
	}
	for i := range calls {
		chunks = append(chunks,
			ai.Chunk{Kind: ai.ChunkToolStart, Tool: &calls[i]},
			ai.Chunk{Kind: ai.ChunkToolEnd, Tool: &calls[i]})
	}
	if len(calls) > 0 {
		stop = ai.StopToolUse
	}
	return append(chunks, ai.Chunk{Kind: ai.ChunkDone, Stop: stop}), nil
}

// partialTag reports how many characters at the end of the text could still
// grow into an opening tag or into an end of turn marker. Holding them back is
// what keeps a tag from being printed one character at a time before there is
// enough of it to recognise.
func partialTag(text string) int {
	keep := 0
	for _, marker := range append([]string{CallOpen}, Endings...) {
		longest := min(len(text), len(marker)-1)
		for length := longest; length > keep; length-- {
			if strings.HasPrefix(marker, text[len(text)-length:]) {
				keep = length
				break
			}
		}
	}
	return keep
}
