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
//
// The marker that opens a turn ends this one, which is not a contradiction. A
// model that writes the beginning of a turn has finished its own and started
// imagining the next: the question it thinks it will be asked, the tool result
// it expects to get back, the answer it would then give. All of it is invented,
// and one of them wrote a page of it.
var Endings = []string{
	"<end_of_turn>",
	"<start_of_turn>",
	"</start_of_turn>",
	"<|start_of_turn>",
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
//
// Whichever comes first decides: a call that begins before the end of the turn
// is a call to make, and an end of turn before any call is the end of the
// answer. Reading it the other way round turns a tool call into a sentence
// printed to the person, tags and all.
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
	_, call := opening(pending)
	ends := earliest(pending, Endings)
	if call >= 0 && (ends < 0 || call < ends) {
		r.calling = true
		r.visible.Reset()
		r.held.WriteString(pending[call:])
		return r.show(pending[:call])
	}
	if ends >= 0 {
		r.ending = true
		r.visible.Reset()
		return r.show(pending[:ends])
	}
	return r.show(r.release(pending))
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

// finished is the held text with anything after the end of the turn taken off.
//
// The live text is cut as it arrives, but from the moment a tool call starts
// nothing is shown until the turn is over, and that held text used to go out
// whole: a call, then the model's own end of turn marker, then the question it
// imagined being asked next and the answer it would have given. All of it
// appeared on the screen at once, which is what the cutting was there to stop.
func (r *reader) finished() string {
	held := r.held.String()
	at := earliest(held, Endings)
	if at < 0 {
		return held
	}
	r.ending = true
	return held[:at]
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
	calls, prose, err := ParseCalls(r.finished())
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
	for _, marker := range append(Openers(), Endings...) {
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
