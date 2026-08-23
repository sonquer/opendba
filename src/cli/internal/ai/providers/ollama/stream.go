package ollama

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/sonquer/tui4db/src/cli/internal/ai"
)

type frame struct {
	Message struct {
		Content   string `json:"content"`
		Thinking  string `json:"thinking"`
		ToolCalls []struct {
			Function struct {
				Name      string         `json:"name"`
				Arguments map[string]any `json:"arguments"`
			} `json:"function"`
		} `json:"tool_calls"`
	} `json:"message"`
	Done            bool   `json:"done"`
	DoneReason      string `json:"done_reason"`
	PromptEvalCount int    `json:"prompt_eval_count"`
	EvalCount       int    `json:"eval_count"`
	Error           string `json:"error"`
}

// stream reads newline delimited json. There is no event framing to speak of,
// so a line is a frame and the end is a frame that says so.
type stream struct {
	instance string
	reader   *bufio.Reader
	body     io.ReadCloser
	pending  []ai.Chunk
	usage    *ai.Usage
	stop     ai.StopReason
	calls    int
	inText   bool
	inThink  bool
	ended    bool
	finished bool
}

func newStream(instance string, body io.ReadCloser) *stream {
	return &stream{
		instance: instance,
		reader:   bufio.NewReader(body),
		body:     body,
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
			s.pending = s.close()
			continue
		}
		line, err := s.reader.ReadString('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			return ai.Chunk{}, ai.Failure(ai.ReasonProvider, s.instance, "read the stream", err)
		}
		last := errors.Is(err, io.EOF)
		chunks, readErr := s.read(strings.TrimSpace(line))
		if readErr != nil {
			return ai.Chunk{}, readErr
		}
		if last {
			s.ended = true
		}
		s.pending = chunks
	}
}

func (s *stream) read(line string) ([]ai.Chunk, error) {
	if line == "" {
		return nil, nil
	}
	var decoded frame
	if err := json.Unmarshal([]byte(line), &decoded); err != nil {
		return nil, ai.Failure(ai.ReasonDecode, s.instance, "read a line of the stream", err)
	}
	if decoded.Error != "" {
		return nil, &ai.Error{
			Reason:   ai.Classify(0, decoded.Error),
			Instance: s.instance,
			Message:  decoded.Error,
		}
	}
	var chunks []ai.Chunk
	if decoded.Message.Thinking != "" {
		chunks = append(chunks, s.thinking(decoded.Message.Thinking)...)
	}
	if decoded.Message.Content != "" {
		chunks = append(chunks, s.saying(decoded.Message.Content)...)
	}
	for _, call := range decoded.Message.ToolCalls {
		chunks = append(chunks, s.calling(call.Function.Name, call.Function.Arguments)...)
	}
	if decoded.Done {
		s.ended = true
		if decoded.DoneReason != "" {
			s.stop = stopReason(decoded.DoneReason)
		}
		if decoded.PromptEvalCount > 0 || decoded.EvalCount > 0 {
			s.usage = &ai.Usage{
				Input:          decoded.PromptEvalCount,
				NonCachedInput: decoded.PromptEvalCount,
				Output:         decoded.EvalCount,
				Total:          decoded.PromptEvalCount + decoded.EvalCount,
			}
		}
	}
	return chunks, nil
}

func (s *stream) saying(text string) []ai.Chunk {
	var chunks []ai.Chunk
	if s.inThink {
		s.inThink = false
		chunks = append(chunks, ai.Chunk{Kind: ai.ChunkReasoningEnd})
	}
	if !s.inText {
		s.inText = true
		chunks = append(chunks, ai.Chunk{Kind: ai.ChunkTextStart})
	}
	return append(chunks, ai.Chunk{Kind: ai.ChunkTextDelta, Text: text})
}

func (s *stream) thinking(text string) []ai.Chunk {
	var chunks []ai.Chunk
	if !s.inThink {
		s.inThink = true
		chunks = append(chunks, ai.Chunk{Kind: ai.ChunkReasoningStart})
	}
	return append(chunks, ai.Chunk{Kind: ai.ChunkReasoningDelta, Text: text})
}

func (s *stream) calling(name string, args map[string]any) []ai.Chunk {
	var chunks []ai.Chunk
	if s.inThink {
		s.inThink = false
		chunks = append(chunks, ai.Chunk{Kind: ai.ChunkReasoningEnd})
	}
	if s.inText {
		s.inText = false
		chunks = append(chunks, ai.Chunk{Kind: ai.ChunkTextEnd})
	}
	s.calls++
	if args == nil {
		args = map[string]any{}
	}
	call := ai.ToolCall{ID: fmt.Sprintf("call_%d", s.calls), Name: name, Arguments: args}
	return append(chunks,
		ai.Chunk{Kind: ai.ChunkToolStart},
		ai.Chunk{Kind: ai.ChunkToolEnd, Tool: &call})
}

func (s *stream) close() []ai.Chunk {
	var chunks []ai.Chunk
	if s.inThink {
		s.inThink = false
		chunks = append(chunks, ai.Chunk{Kind: ai.ChunkReasoningEnd})
	}
	if s.inText {
		s.inText = false
		chunks = append(chunks, ai.Chunk{Kind: ai.ChunkTextEnd})
	}
	if s.calls > 0 {
		s.stop = ai.StopToolUse
	}
	return append(chunks, ai.Chunk{Kind: ai.ChunkDone, Stop: s.stop, Usage: s.usage})
}

func stopReason(from string) ai.StopReason {
	switch from {
	case "length":
		return ai.StopMaxTokens
	default:
		return ai.StopEndTurn
	}
}

// Close ends the stream early.
func (s *stream) Close() error { return s.body.Close() }
