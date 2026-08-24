// Package openai speaks the chat completions protocol.
package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/sonquer/tui4db/src/cli/internal/ai"
)

// DefaultEndpoint is where OpenAI itself answers.
const DefaultEndpoint = "https://api.openai.com/v1"

const defaultMaxTokens = 4096

// Provider opens clients that speak chat completions.
type Provider struct {
	kind     ai.Kind
	endpoint string
}

// New returns the provider for OpenAI.
func New() *Provider { return &Provider{kind: ai.KindOpenAI, endpoint: DefaultEndpoint} }

// Compatible returns the provider for any other endpoint that answers the same
// requests, which the user configures with an address of their own.
func Compatible() *Provider { return &Provider{kind: ai.KindCompatible} }

// Kind names this back-end.
func (p *Provider) Kind() ai.Kind { return p.kind }

// Open returns a client for an instance.
func (p *Provider) Open(instance ai.Instance, deps ai.Deps) (ai.Client, error) {
	if deps.HTTP == nil {
		return nil, fmt.Errorf("reaching %s needs an http client", instance.Name)
	}
	endpoint := strings.TrimRight(instance.Endpoint, "/")
	if endpoint == "" {
		endpoint = p.endpoint
	}
	if endpoint == "" {
		return nil, fmt.Errorf("instance %s needs an endpoint", instance.Name)
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
		Local:     loopback(c.endpoint),
		Context:   c.instance.Context,
	}
}

// loopback reports whether an endpoint is on this machine, which is what
// decides whether anything actually leaves it.
func loopback(endpoint string) bool {
	lowered := strings.ToLower(endpoint)
	for _, host := range []string{"//localhost", "//127.0.0.1", "//[::1]", "//0.0.0.0"} {
		if strings.Contains(lowered, host) {
			return true
		}
	}
	return false
}

// Probe asks for the model list, which costs nothing and fails in the same way
// a real request would when the key is wrong.
func (c *client) Probe(ctx context.Context) error {
	_, err := c.Models(ctx)
	return err
}

// Models reports what the endpoint is serving.
func (c *client) Models(ctx context.Context) ([]ai.ModelInfo, error) {
	response, err := c.transport.Do(ctx, func() (*http.Request, error) {
		request, err := http.NewRequest(http.MethodGet, c.endpoint+"/models", nil)
		if err != nil {
			return nil, err
		}
		c.sign(request)
		return request, nil
	})
	if err != nil {
		return nil, err
	}
	defer func() { _ = response.Body.Close() }()

	var listed struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(response.Body).Decode(&listed); err != nil {
		return nil, ai.Failure(ai.ReasonDecode, c.instance.Name, "read the model list", err)
	}
	models := make([]ai.ModelInfo, 0, len(listed.Data))
	for _, model := range listed.Data {
		models = append(models, ai.ModelInfo{ID: model.ID, Title: model.ID})
	}
	return models, nil
}

// Chat starts an answer.
func (c *client) Chat(ctx context.Context, req ai.Request) (ai.Stream, error) {
	body, err := json.Marshal(c.body(req))
	if err != nil {
		return nil, ai.Failure(ai.ReasonRequest, c.instance.Name, "write the request", err)
	}
	response, err := c.transport.Do(ctx, func() (*http.Request, error) {
		request, err := http.NewRequest(http.MethodPost, c.endpoint+"/chat/completions", bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		c.sign(request)
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Accept", "text/event-stream")
		return request, nil
	})
	if err != nil {
		return nil, err
	}
	return newStream(c.instance.Name, response.Body), nil
}

func (c *client) sign(request *http.Request) {
	if len(c.instance.Key) > 0 {
		request.Header.Set("Authorization", "Bearer "+string(c.instance.Key))
	}
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
		"model":                 model,
		"messages":              messages(req),
		"stream":                true,
		"max_completion_tokens": maxTokens,
		"stream_options":        map[string]any{"include_usage": true},
	}
	if req.Temperature > 0 {
		body["temperature"] = req.Temperature
	}
	if req.TopP > 0 {
		body["top_p"] = req.TopP
	}
	if len(req.Stop) > 0 {
		body["stop"] = req.Stop
	}
	if len(req.Tools) > 0 {
		body["tools"] = tools(req.Tools)
	}
	return body
}

func messages(req ai.Request) []map[string]any {
	out := make([]map[string]any, 0, len(req.Messages)+1)
	if system := strings.TrimSpace(req.System); system != "" {
		out = append(out, map[string]any{"role": "system", "content": system})
	}
	for _, message := range req.Messages {
		out = append(out, written(message)...)
	}
	return out
}

func written(message ai.Message) []map[string]any {
	switch message.Role {
	case ai.RoleTool:
		if message.Result == nil {
			return nil
		}
		return []map[string]any{{
			"role":         "tool",
			"tool_call_id": message.Result.ID,
			"content":      message.Result.Content,
		}}
	case ai.RoleAssistant:
		written := map[string]any{"role": "assistant", "content": message.Content}
		if len(message.Calls) > 0 {
			written["tool_calls"] = calls(message.Calls)
		}
		return []map[string]any{written}
	default:
		return []map[string]any{{"role": string(message.Role), "content": message.Content}}
	}
}

func calls(from []ai.ToolCall) []map[string]any {
	out := make([]map[string]any, 0, len(from))
	for _, call := range from {
		arguments, err := json.Marshal(call.Arguments)
		if err != nil {
			continue
		}
		out = append(out, map[string]any{
			"id":   call.ID,
			"type": "function",
			"function": map[string]any{
				"name":      call.Name,
				"arguments": string(arguments),
			},
		})
	}
	return out
}

func tools(from []ai.Tool) []map[string]any {
	out := make([]map[string]any, 0, len(from))
	for _, tool := range from {
		out = append(out, map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":        tool.Name,
				"description": tool.Description,
				"parameters":  tool.Parameters.JSON(),
			},
		})
	}
	return out
}

var _ io.Closer = (*stream)(nil)
