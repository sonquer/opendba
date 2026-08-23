package anthropic

import (
	"encoding/json"
	"errors"
	"io"
	"strings"

	"github.com/sonquer/tui4db/src/cli/internal/ai"
)

type frame struct {
	Type    string `json:"type"`
	Index   int    `json:"index"`
	Message *struct {
		Usage *tally `json:"usage"`
	} `json:"message"`
	ContentBlock *struct {
		Type string `json:"type"`
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"content_block"`
	Delta *struct {
		Type        string `json:"type"`
		Text        string `json:"text"`
		Thinking    string `json:"thinking"`
		PartialJSON string `json:"partial_json"`
		StopReason  string `json:"stop_reason"`
	} `json:"delta"`
	Usage *tally `json:"usage"`
	Error *struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error"`
}

type tally struct {
	InputTokens      int `json:"input_tokens"`
	OutputTokens     int `json:"output_tokens"`
	CacheReadTokens  int `json:"cache_read_input_tokens"`
	CacheWriteTokens int `json:"cache_creation_input_tokens"`
}

type block struct {
	kind      string
	id        string
	name      string
	arguments strings.Builder
}

// stream reads message events and turns them into chunks. The blocks are
// already bracketed on the wire, so the work here is mostly to carry the
// arguments of a tool block until the block closes and they can be read.
type stream struct {
	instance string
	events   *ai.Events
	pending  []ai.Chunk
	blocks   map[int]*block
	usage    ai.Usage
	stop     ai.StopReason
	ended    bool
	finished bool
}

func newStream(instance string, body io.ReadCloser) *stream {
	return &stream{
		instance: instance,
		events:   ai.NewEvents(body),
		blocks:   map[int]*block{},
		stop:     ai.StopEndTurn,
	}
}

// Next returns the next chunk, or io.EOF when the answer is finished.
func (s *stream) Next() (ai.Chunk, error) {
	for {
		if len(s.pending) > 0 {
			chunk := s.pending[0]
			s.pending = s.pending[1:]
			return chunk, nil
		}
		if s.finished {
			return ai.Chunk{}, io.EOF
		}
		if s.ended {
			s.finished = true
			s.pending = []ai.Chunk{{Kind: ai.ChunkDone, Stop: s.stop, Usage: s.total()}}
			continue
		}
		event, err := s.events.Next()
		if errors.Is(err, io.EOF) {
			s.ended = true
			continue
		}
		if err != nil {
			return ai.Chunk{}, ai.Failure(ai.ReasonProvider, s.instance, "read the stream", err)
		}
		chunks, err := s.read(event)
		if err != nil {
			return ai.Chunk{}, err
		}
		s.pending = chunks
	}
}

func (s *stream) read(event ai.Event) ([]ai.Chunk, error) {
	if strings.TrimSpace(event.Data) == "" {
		return nil, nil
	}
	var decoded frame
	if err := json.Unmarshal([]byte(event.Data), &decoded); err != nil {
		return nil, ai.Failure(ai.ReasonDecode, s.instance, "read a frame of the stream", err)
	}
	switch decoded.Type {
	case "error":
		return nil, s.failure(decoded)
	case "message_start":
		if decoded.Message != nil {
			s.count(decoded.Message.Usage)
		}
	case "content_block_start":
		return s.open(decoded), nil
	case "content_block_delta":
		return s.fill(decoded), nil
	case "content_block_stop":
		return s.shut(decoded.Index), nil
	case "message_delta":
		if decoded.Delta != nil && decoded.Delta.StopReason != "" {
			s.stop = stopReason(decoded.Delta.StopReason)
		}
		s.count(decoded.Usage)
	case "message_stop":
		s.ended = true
	}
	return nil, nil
}

func (s *stream) failure(decoded frame) error {
	message := ""
	if decoded.Error != nil {
		message = decoded.Error.Message
	}
	return &ai.Error{
		Reason:   ai.Classify(0, message),
		Instance: s.instance,
		Message:  message,
	}
}

func (s *stream) open(decoded frame) []ai.Chunk {
	if decoded.ContentBlock == nil {
		return nil
	}
	held := &block{kind: decoded.ContentBlock.Type, id: decoded.ContentBlock.ID, name: decoded.ContentBlock.Name}
	s.blocks[decoded.Index] = held
	switch held.kind {
	case "thinking", "redacted_thinking":
		return []ai.Chunk{{Kind: ai.ChunkReasoningStart}}
	case "tool_use":
		return []ai.Chunk{{Kind: ai.ChunkToolStart}}
	default:
		return []ai.Chunk{{Kind: ai.ChunkTextStart}}
	}
}

func (s *stream) fill(decoded frame) []ai.Chunk {
	held, known := s.blocks[decoded.Index]
	if !known || decoded.Delta == nil {
		return nil
	}
	switch decoded.Delta.Type {
	case "thinking_delta":
		return []ai.Chunk{{Kind: ai.ChunkReasoningDelta, Text: decoded.Delta.Thinking}}
	case "input_json_delta":
		held.arguments.WriteString(decoded.Delta.PartialJSON)
		return []ai.Chunk{{Kind: ai.ChunkToolDelta, Text: decoded.Delta.PartialJSON}}
	case "text_delta":
		return []ai.Chunk{{Kind: ai.ChunkTextDelta, Text: decoded.Delta.Text}}
	default:
		return nil
	}
}

func (s *stream) shut(index int) []ai.Chunk {
	held, known := s.blocks[index]
	if !known {
		return nil
	}
	delete(s.blocks, index)
	switch held.kind {
	case "thinking", "redacted_thinking":
		return []ai.Chunk{{Kind: ai.ChunkReasoningEnd}}
	case "tool_use":
		call := ai.ToolCall{ID: held.id, Name: held.name, Arguments: arguments(held.arguments.String())}
		return []ai.Chunk{{Kind: ai.ChunkToolEnd, Tool: &call}}
	default:
		return []ai.Chunk{{Kind: ai.ChunkTextEnd}}
	}
}

// count gathers the two halves of the accounting. The input is reported once at
// the start and the output once at the end, and the parts that do not overlap
// are worked out here so that nobody downstream has to subtract.
func (s *stream) count(from *tally) {
	if from == nil {
		return
	}
	if from.InputTokens > 0 {
		s.usage.Input = from.InputTokens
	}
	if from.OutputTokens > 0 {
		s.usage.Output = from.OutputTokens
	}
	if from.CacheReadTokens > 0 {
		s.usage.CacheRead = from.CacheReadTokens
	}
	if from.CacheWriteTokens > 0 {
		s.usage.CacheWrite = from.CacheWriteTokens
	}
}

func (s *stream) total() *ai.Usage {
	usage := s.usage
	usage.Input += usage.CacheRead + usage.CacheWrite
	usage.NonCachedInput = max(usage.Input-usage.CacheRead-usage.CacheWrite, 0)
	usage.Total = usage.Input + usage.Output
	return &usage
}

func arguments(written string) map[string]any {
	if strings.TrimSpace(written) == "" {
		return map[string]any{}
	}
	var decoded map[string]any
	if err := json.Unmarshal([]byte(written), &decoded); err != nil {
		return map[string]any{}
	}
	return decoded
}

func stopReason(from string) ai.StopReason {
	switch from {
	case "max_tokens":
		return ai.StopMaxTokens
	case "tool_use":
		return ai.StopToolUse
	case "refusal":
		return ai.StopContentFilter
	default:
		return ai.StopEndTurn
	}
}

// Close ends the stream early.
func (s *stream) Close() error { return s.events.Close() }
