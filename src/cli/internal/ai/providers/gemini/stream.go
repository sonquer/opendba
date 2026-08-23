package gemini

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/sonquer/tui4db/src/cli/internal/ai"
)

type frame struct {
	Candidates []struct {
		Content struct {
			Parts []struct {
				Text         string `json:"text"`
				Thought      bool   `json:"thought"`
				FunctionCall *struct {
					Name string         `json:"name"`
					Args map[string]any `json:"args"`
				} `json:"functionCall"`
			} `json:"parts"`
		} `json:"content"`
		FinishReason string `json:"finishReason"`
	} `json:"candidates"`
	UsageMetadata *struct {
		PromptTokenCount     int `json:"promptTokenCount"`
		CandidatesTokenCount int `json:"candidatesTokenCount"`
		TotalTokenCount      int `json:"totalTokenCount"`
		CachedContentTokens  int `json:"cachedContentTokenCount"`
		ThoughtsTokenCount   int `json:"thoughtsTokenCount"`
	} `json:"usageMetadata"`
	Error *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Status  string `json:"status"`
	} `json:"error"`
}

// stream reads generated content and turns it into chunks. A part carries whole
// text rather than a delta and a call arrives complete, so the bracketing that
// the other protocols send is worked out here instead.
type stream struct {
	instance string
	events   *ai.Events
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
		events:   ai.NewEvents(body),
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
		chunks, err := s.read(strings.TrimSpace(event.Data))
		if err != nil {
			return ai.Chunk{}, err
		}
		s.pending = chunks
	}
}

func (s *stream) read(data string) ([]ai.Chunk, error) {
	if data == "" {
		return nil, nil
	}
	var decoded frame
	if err := json.Unmarshal([]byte(data), &decoded); err != nil {
		return nil, ai.Failure(ai.ReasonDecode, s.instance, "read a frame of the stream", err)
	}
	if decoded.Error != nil {
		return nil, &ai.Error{
			Reason:   ai.Classify(decoded.Error.Code, decoded.Error.Message),
			Instance: s.instance,
			Status:   decoded.Error.Code,
			Message:  decoded.Error.Message,
		}
	}
	if decoded.UsageMetadata != nil {
		s.usage = &ai.Usage{
			Input:          decoded.UsageMetadata.PromptTokenCount,
			Output:         decoded.UsageMetadata.CandidatesTokenCount + decoded.UsageMetadata.ThoughtsTokenCount,
			Total:          decoded.UsageMetadata.TotalTokenCount,
			CacheRead:      decoded.UsageMetadata.CachedContentTokens,
			NonCachedInput: max(decoded.UsageMetadata.PromptTokenCount-decoded.UsageMetadata.CachedContentTokens, 0),
			Reasoning:      decoded.UsageMetadata.ThoughtsTokenCount,
		}
	}
	var chunks []ai.Chunk
	for _, candidate := range decoded.Candidates {
		for _, part := range candidate.Content.Parts {
			switch {
			case part.FunctionCall != nil:
				chunks = append(chunks, s.calling(part.FunctionCall.Name, part.FunctionCall.Args)...)
			case part.Thought && part.Text != "":
				chunks = append(chunks, s.thinking(part.Text)...)
			case part.Text != "":
				chunks = append(chunks, s.saying(part.Text)...)
			}
		}
		if candidate.FinishReason != "" {
			s.stop = stopReason(candidate.FinishReason)
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

// calling hands over a whole call at once. This protocol does not stream the
// arguments a fragment at a time, so the start and the end arrive together.
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
	case "MAX_TOKENS":
		return ai.StopMaxTokens
	case "SAFETY", "RECITATION", "PROHIBITED_CONTENT", "BLOCKLIST":
		return ai.StopContentFilter
	default:
		return ai.StopEndTurn
	}
}

// Close ends the stream early.
func (s *stream) Close() error { return s.events.Close() }
