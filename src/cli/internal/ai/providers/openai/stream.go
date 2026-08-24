package openai

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/sonquer/opendba/src/cli/internal/ai"
)

const doneSentinel = "[DONE]"

type frame struct {
	Choices []struct {
		Delta struct {
			Content          string `json:"content"`
			ReasoningContent string `json:"reasoning_content"`
			Reasoning        string `json:"reasoning"`
			ToolCalls        []struct {
				Index    int    `json:"index"`
				ID       string `json:"id"`
				Function struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls"`
		} `json:"delta"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage *struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
		PromptDetails    struct {
			CachedTokens int `json:"cached_tokens"`
		} `json:"prompt_tokens_details"`
		CompletionDetails struct {
			ReasoningTokens int `json:"reasoning_tokens"`
		} `json:"completion_tokens_details"`
	} `json:"usage"`
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error"`
}

type building struct {
	id        string
	name      string
	arguments strings.Builder
}

// stream reads chat completion frames and turns them into chunks.
type stream struct {
	instance string
	events   *ai.Events
	pending  []ai.Chunk
	order    []int
	tools    map[int]*building
	usage    *ai.Usage
	stop     ai.StopReason
	inText   bool
	inThink  bool
	ended    bool
	finished bool
}

func newStream(instance string, body io.ReadCloser) *stream {
	return &stream{
		instance: instance,
		events:   ai.NewEvents(body),
		tools:    map[int]*building{},
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
		event, err := s.events.Next()
		if errors.Is(err, io.EOF) {
			s.ended = true
			continue
		}
		if err != nil {
			return ai.Chunk{}, ai.Failure(ai.ReasonProvider, s.instance, "read the stream", err)
		}
		data := strings.TrimSpace(event.Data)
		if data == "" {
			continue
		}
		if data == doneSentinel {
			s.ended = true
			continue
		}
		chunks, err := s.read(data)
		if err != nil {
			return ai.Chunk{}, err
		}
		s.pending = chunks
	}
}

func (s *stream) read(data string) ([]ai.Chunk, error) {
	var decoded frame
	if err := json.Unmarshal([]byte(data), &decoded); err != nil {
		return nil, ai.Failure(ai.ReasonDecode, s.instance, "read a frame of the stream", err)
	}
	if decoded.Error != nil {
		return nil, &ai.Error{
			Reason:   ai.Classify(0, decoded.Error.Message),
			Instance: s.instance,
			Message:  decoded.Error.Message,
		}
	}
	if decoded.Usage != nil {
		s.usage = &ai.Usage{
			Input:          decoded.Usage.PromptTokens,
			Output:         decoded.Usage.CompletionTokens,
			Total:          decoded.Usage.TotalTokens,
			CacheRead:      decoded.Usage.PromptDetails.CachedTokens,
			NonCachedInput: max(decoded.Usage.PromptTokens-decoded.Usage.PromptDetails.CachedTokens, 0),
			Reasoning:      decoded.Usage.CompletionDetails.ReasoningTokens,
		}
	}
	var chunks []ai.Chunk
	for _, choice := range decoded.Choices {
		chunks = append(chunks, s.thinking(choice.Delta.ReasoningContent+choice.Delta.Reasoning)...)
		chunks = append(chunks, s.saying(choice.Delta.Content)...)
		for _, call := range choice.Delta.ToolCalls {
			chunks = append(chunks, s.calling(call.Index, call.ID, call.Function.Name, call.Function.Arguments)...)
		}
		if choice.FinishReason != "" {
			s.stop = stopReason(choice.FinishReason)
		}
	}
	return chunks, nil
}

func (s *stream) saying(text string) []ai.Chunk {
	if text == "" {
		return nil
	}
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
	if text == "" {
		return nil
	}
	var chunks []ai.Chunk
	if !s.inThink {
		s.inThink = true
		chunks = append(chunks, ai.Chunk{Kind: ai.ChunkReasoningStart})
	}
	return append(chunks, ai.Chunk{Kind: ai.ChunkReasoningDelta, Text: text})
}

func (s *stream) calling(index int, id, name, arguments string) []ai.Chunk {
	var chunks []ai.Chunk
	if s.inText {
		s.inText = false
		chunks = append(chunks, ai.Chunk{Kind: ai.ChunkTextEnd})
	}
	call, known := s.tools[index]
	if !known {
		call = &building{}
		s.tools[index] = call
		s.order = append(s.order, index)
		chunks = append(chunks, ai.Chunk{Kind: ai.ChunkToolStart})
	}
	if id != "" {
		call.id = id
	}
	if name != "" {
		call.name = name
	}
	if arguments != "" {
		call.arguments.WriteString(arguments)
		chunks = append(chunks, ai.Chunk{Kind: ai.ChunkToolDelta, Text: arguments})
	}
	return chunks
}

// close ends whatever block was open, hands over the calls that were gathered,
// and says why the answer stopped.
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
	for _, index := range s.order {
		built := s.tools[index]
		call := ai.ToolCall{ID: built.id, Name: built.name, Arguments: arguments(built.arguments.String())}
		if call.ID == "" {
			call.ID = fmt.Sprintf("call_%d", index+1)
		}
		chunks = append(chunks, ai.Chunk{Kind: ai.ChunkToolEnd, Tool: &call})
	}
	if len(s.order) > 0 {
		s.stop = ai.StopToolUse
	}
	return append(chunks, ai.Chunk{Kind: ai.ChunkDone, Stop: s.stop, Usage: s.usage})
}

// arguments reads the JSON a model wrote a fragment at a time.
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

func stopReason(finish string) ai.StopReason {
	switch finish {
	case "length":
		return ai.StopMaxTokens
	case "tool_calls", "function_call":
		return ai.StopToolUse
	case "content_filter":
		return ai.StopContentFilter
	default:
		return ai.StopEndTurn
	}
}

// Close ends the stream early.
func (s *stream) Close() error { return s.events.Close() }
