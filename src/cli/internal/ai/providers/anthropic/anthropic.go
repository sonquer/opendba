// Package anthropic speaks the messages protocol.
package anthropic

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/sonquer/opendba/src/cli/internal/ai"
)

// DefaultEndpoint is where Anthropic answers.
const DefaultEndpoint = "https://api.anthropic.com/v1"

// Version is the dated contract the request is written against. It is sent on
// every request, and a change to it is a change to this file.
const Version = "2023-06-01"

const (
	defaultMaxTokens = 4096
	minThinking      = 1024
)

// Provider opens clients that speak the messages protocol.
type Provider struct{}

// New returns the provider.
func New() *Provider { return &Provider{} }

// Kind names this back-end.
func (p *Provider) Kind() ai.Kind { return ai.KindAnthropic }

// Open returns a client for an instance.
func (p *Provider) Open(instance ai.Instance, deps ai.Deps) (ai.Client, error) {
	if deps.HTTP == nil {
		return nil, fmt.Errorf("reaching %s needs an http client", instance.Name)
	}
	endpoint := strings.TrimRight(instance.Endpoint, "/")
	if endpoint == "" {
		endpoint = DefaultEndpoint
	}
	return &client{
		instance:  instance,
		endpoint:  endpoint,
		transport: ai.NewTransport(deps.HTTP, instance.Name),
	}, nil
}

type client struct {
	instance  ai.Instance
	endpoint  string
	transport *ai.Transport
}

// Capabilities reports what this protocol offers.
func (c *client) Capabilities() ai.Capabilities {
	return ai.Capabilities{
		Tools:     true,
		Streaming: true,
		Reasoning: true,
		Context:   c.instance.Context,
	}
}

// Probe sends the smallest answerable request there is. There is no free model
// list on this protocol, so being sure the key works costs a token or two.
func (c *client) Probe(ctx context.Context) error {
	stream, err := c.Chat(ctx, ai.Request{
		Messages:  []ai.Message{{Role: ai.RoleUser, Content: "ping"}},
		MaxTokens: 1,
	})
	if err != nil {
		return err
	}
	return stream.Close()
}

// Chat starts an answer.
func (c *client) Chat(ctx context.Context, req ai.Request) (ai.Stream, error) {
	body, err := json.Marshal(c.body(req))
	if err != nil {
		return nil, ai.Failure(ai.ReasonRequest, c.instance.Name, "write the request", err)
	}
	response, err := c.transport.Do(ctx, func() (*http.Request, error) {
		request, err := http.NewRequest(http.MethodPost, c.endpoint+"/messages", bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		if len(c.instance.Key) > 0 {
			request.Header.Set("X-Api-Key", string(c.instance.Key))
		}
		request.Header.Set("Anthropic-Version", Version)
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Accept", "text/event-stream")
		return request, nil
	})
	if err != nil {
		return nil, err
	}
	return newStream(c.instance.Name, response.Body), nil
}

func (c *client) body(req ai.Request) map[string]any {
	model := req.Model
	if model == "" {
		model = c.instance.Model
	}
	maxTokens := req.MaxTokens
	if maxTokens <= 0 {
		maxTokens = defaultMaxTokens
	}
	body := map[string]any{
		"model":      model,
		"max_tokens": maxTokens,
		"messages":   messages(req.Messages),
		"stream":     true,
	}
	if system := strings.TrimSpace(req.System); system != "" {
		body["system"] = system
	}
	if req.Temperature > 0 {
		body["temperature"] = req.Temperature
	}
	if req.TopP > 0 {
		body["top_p"] = req.TopP
	}
	if len(req.Stop) > 0 {
		body["stop_sequences"] = req.Stop
	}
	if len(req.Tools) > 0 {
		body["tools"] = tools(req.Tools)
	}
	if req.Thinking {
		body["thinking"] = map[string]any{"type": "enabled", "budget_tokens": budget(maxTokens)}
	}
	return body
}

// budget keeps the room set aside for thinking inside what the answer is
// allowed to be, since the two come out of the same allowance.
func budget(maxTokens int) int {
	half := maxTokens / 2
	if half < minThinking {
		return minThinking
	}
	return half
}

// messages folds the conversation into the alternating turns this protocol
// expects.
func messages(from []ai.Message) []map[string]any {
	out := make([]map[string]any, 0, len(from))
	for _, message := range from {
		blocks, role := content(message)
		if len(blocks) == 0 {
			continue
		}
		if last := len(out) - 1; last >= 0 && out[last]["role"] == role && role == "user" {
			out[last]["content"] = append(out[last]["content"].([]map[string]any), blocks...)
			continue
		}
		out = append(out, map[string]any{"role": role, "content": blocks})
	}
	return out
}

func content(message ai.Message) ([]map[string]any, string) {
	switch message.Role {
	case ai.RoleTool:
		if message.Result == nil {
			return nil, "user"
		}
		return []map[string]any{{
			"type":        "tool_result",
			"tool_use_id": message.Result.ID,
			"content":     message.Result.Content,
			"is_error":    message.Result.Failed,
		}}, "user"
	case ai.RoleAssistant:
		blocks := make([]map[string]any, 0, len(message.Calls)+1)
		if text := strings.TrimSpace(message.Content); text != "" {
			blocks = append(blocks, map[string]any{"type": "text", "text": text})
		}
		for _, call := range message.Calls {
			blocks = append(blocks, map[string]any{
				"type":  "tool_use",
				"id":    call.ID,
				"name":  call.Name,
				"input": call.Arguments,
			})
		}
		return blocks, "assistant"
	default:
		if text := strings.TrimSpace(message.Content); text != "" {
			return []map[string]any{{"type": "text", "text": text}}, "user"
		}
		return nil, "user"
	}
}

func tools(from []ai.Tool) []map[string]any {
	out := make([]map[string]any, 0, len(from))
	for _, tool := range from {
		out = append(out, map[string]any{
			"name":         tool.Name,
			"description":  tool.Description,
			"input_schema": tool.Parameters.JSON(),
		})
	}
	return out
}
