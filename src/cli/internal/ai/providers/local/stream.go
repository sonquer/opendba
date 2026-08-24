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

	// thinking is the model talking to itself rather than to the person, and
	// thought is that having been started so it can be closed. They are kept
	// apart from started for the same reason the chunks are: a block of
	// deliberation must never open a block of answer.
	thinking bool
	thought  bool

	// naming is the channel's own name not yet having been read off the front
	// of what was said. It is a state rather than a cut made once, because the
	// name arrives a byte at a time like everything else and a cut made on the
	// first of those bytes cuts nothing.
	naming bool

	// speaks is the family whose brackets were recognised, kept so the closing
	// one looked for is the mate of the opening one found.
	speaks dialect

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
	if r.thinking {
		return r.deliberating()
	}
	pending := r.visible.String()
	_, call := opening(pending)
	ends := earliest(pending, Endings)
	spoken, thinks := thinkingOpens(pending)
	if thinks >= 0 && first(thinks, call, ends) {
		r.thinking, r.naming, r.speaks = true, true, spoken
		r.visible.Reset()
		r.visible.WriteString(pending[thinks+len(spoken.thinkOpen):])
		return append(r.show(pending[:thinks]), r.deliberating()...)
	}
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

// first reports whether a marker is the earliest of the three, counting the
// ones that are not there at all as being nowhere.
func first(at int, others ...int) bool {
	for _, other := range others {
		if other >= 0 && other < at {
			return false
		}
	}
	return true
}

// withoutChannelName drops the name of the channel from the front of what the
// model said to itself, and says whether it could tell yet.
//
// A channel is opened as <|channel>thought, and the word after the bracket
// names the channel rather than being the first thing thought. A word is only
// the name when something spaces it from what follows: <|channel>why is a
// channel with no name and a thought that begins at "why", and eating that word
// would be eating the thought.
func withoutChannelName(text string) (string, bool) {
	cut := 0
	for cut < len(text) && isLetter(text[cut]) {
		cut++
	}
	if cut == len(text) {
		return text, false
	}
	if !isSpacer(text[cut]) {
		return text, true
	}
	return strings.TrimLeft(text[cut:], " \t\r\n"), true
}

func isLetter(held byte) bool {
	return held >= 'a' && held <= 'z' || held >= 'A' && held <= 'Z'
}

func isSpacer(held byte) bool {
	return held == ' ' || held == '\t' || held == '\r' || held == '\n'
}

// deliberating forwards what the model is saying to itself, holding back the
// tail that could still turn into the closing bracket and stopping the moment
// it does. The words stream rather than being kept to the end: the screen shows
// the last of them while they are the only thing that has arrived, and a model
// that thinks for a minute with nothing on screen is a model that has hung.
func (r *reader) deliberating() []ai.Chunk {
	if r.naming {
		rest, told := withoutChannelName(r.visible.String())
		if !told {
			return nil
		}
		r.naming = false
		r.visible.Reset()
		r.visible.WriteString(rest)
	}
	pending := r.visible.String()
	at := strings.Index(pending, r.speaks.thinkClose)
	if at < 0 {
		return r.think(r.release(pending))
	}
	rest := pending[at+len(r.speaks.thinkClose):]
	chunks := r.think(pending[:at])
	if r.thought {
		chunks = append(chunks, ai.Chunk{Kind: ai.ChunkReasoningEnd})
		r.thought = false
	}
	r.thinking = false
	r.visible.Reset()
	if rest == "" {
		return chunks
	}
	return append(chunks, r.add(rest)...)
}

// think is show for what a model says to itself.
func (r *reader) think(text string) []ai.Chunk {
	if text == "" {
		return nil
	}
	chunks := make([]ai.Chunk, 0, 2)
	if !r.thought {
		r.thought = true
		chunks = append(chunks, ai.Chunk{Kind: ai.ChunkReasoningStart})
	}
	return append(chunks, ai.Chunk{Kind: ai.ChunkReasoningDelta, Text: text})
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
	if r.thinking {
		said := r.visible.String()
		if r.naming {
			said, _ = withoutChannelName(said)
		}
		chunks = append(chunks, r.think(said)...)
		if r.thought {
			chunks = append(chunks, ai.Chunk{Kind: ai.ChunkReasoningEnd})
		}
		if r.started {
			chunks = append(chunks, ai.Chunk{Kind: ai.ChunkTextEnd})
		}
		return append(chunks, ai.Chunk{Kind: ai.ChunkDone, Stop: stop}), nil
	}
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
	for _, marker := range append(append(Openers(), Thinking()...), Endings...) {
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
