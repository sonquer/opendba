// Package ollama speaks to a daemon the user runs themselves. It is the one
// back-end that answers in newline delimited json rather than an event stream,
// and the one whose models are already on the machine, so it can say what it
// has rather than being told.
package ollama

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/sonquer/tui4db/src/cli/internal/ai"
)

// DefaultEndpoint is where the daemon listens.
const DefaultEndpoint = "http://localhost:11434"

// Provider opens clients that speak to an ollama daemon.
type Provider struct{}

// New returns the provider.
func New() *Provider { return &Provider{} }

// Kind names this back-end.
func (p *Provider) Kind() ai.Kind { return ai.KindOllama }

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

// Capabilities reports what this back-end offers. A daemon on this machine
// sends nothing anywhere, which is what decides whether a turn has to be
// approved before it is sent.
func (c *client) Capabilities() ai.Capabilities {
	return ai.Capabilities{
		Tools:     true,
		Streaming: true,
		Reasoning: true,
		Local:     loopback(c.endpoint),
		Context:   c.instance.Context,
	}
}

func loopback(endpoint string) bool {
	lowered := strings.ToLower(endpoint)
	for _, host := range []string{"//localhost", "//127.0.0.1", "//[::1]", "//0.0.0.0"} {
		if strings.Contains(lowered, host) {
			return true
		}
	}
	return false
}

// Probe asks the daemon what it has, which answers both whether it is running
// and whether the model this instance names has been pulled.
func (c *client) Probe(ctx context.Context) error {
	models, err := c.Models(ctx)
	if err != nil {
		return err
	}
	if c.instance.Model == "" {
		return nil
	}
	for _, model := range models {
		if model.ID == c.instance.Model || strings.HasPrefix(model.ID, c.instance.Model+":") {
			return nil
		}
	}
	return ai.Failure(ai.ReasonRequest, c.instance.Name,
		fmt.Sprintf("the daemon has not pulled %q", c.instance.Model), nil)
}

// Models reports what the daemon has on disk.
func (c *client) Models(ctx context.Context) ([]ai.ModelInfo, error) {
	response, err := c.transport.Do(ctx, func() (*http.Request, error) {
		return http.NewRequest(http.MethodGet, c.endpoint+"/api/tags", nil)
	})
	if err != nil {
		return nil, err
	}
	defer func() { _ = response.Body.Close() }()

	var listed struct {
		Models []struct {
			Name    string `json:"name"`
			Details struct {
				ParameterSize     string `json:"parameter_size"`
				QuantizationLevel string `json:"quantization_level"`
			} `json:"details"`
		} `json:"models"`
	}
	if err := json.NewDecoder(response.Body).Decode(&listed); err != nil {
		return nil, ai.Failure(ai.ReasonDecode, c.instance.Name, "read the model list", err)
	}
	models := make([]ai.ModelInfo, 0, len(listed.Models))
	for _, model := range listed.Models {
		models = append(models, ai.ModelInfo{ID: model.Name, Title: title(model.Name, model.Details.ParameterSize, model.Details.QuantizationLevel)})
	}
	return models, nil
}

func title(name, size, quantisation string) string {
	parts := []string{name}
	if size != "" {
		parts = append(parts, size)
	}
	if quantisation != "" {
		parts = append(parts, quantisation)
	}
	return strings.Join(parts, " · ")
}

// Chat starts an answer.
func (c *client) Chat(ctx context.Context, req ai.Request) (ai.Stream, error) {
	body, err := json.Marshal(c.body(req))
	if err != nil {
		return nil, ai.Failure(ai.ReasonRequest, c.instance.Name, "write the request", err)
	}
	response, err := c.transport.Do(ctx, func() (*http.Request, error) {
		request, err := http.NewRequest(http.MethodPost, c.endpoint+"/api/chat", bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		request.Header.Set("Content-Type", "application/json")
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
	options := map[string]any{}
	if req.Temperature > 0 {
		options["temperature"] = req.Temperature
	}
	if req.TopP > 0 {
		options["top_p"] = req.TopP
	}
	if req.TopK > 0 {
		options["top_k"] = req.TopK
	}
	if req.MaxTokens > 0 {
		options["num_predict"] = req.MaxTokens
	}
	if c.instance.Context > 0 {
		options["num_ctx"] = c.instance.Context
	}
	if len(req.Stop) > 0 {
		options["stop"] = req.Stop
	}
	body := map[string]any{
		"model":    model,
		"messages": messages(req),
		"stream":   true,
	}
	if len(options) > 0 {
		body["options"] = options
	}
	if len(req.Tools) > 0 {
		body["tools"] = tools(req.Tools)
	}
	if req.Thinking {
		body["think"] = true
	}
	return body
}

func messages(req ai.Request) []map[string]any {
	out := make([]map[string]any, 0, len(req.Messages)+1)
	if system := strings.TrimSpace(req.System); system != "" {
		out = append(out, map[string]any{"role": "system", "content": system})
	}
	for _, message := range req.Messages {
		written := written(message)
		if written != nil {
			out = append(out, written)
		}
	}
	return out
}

func written(message ai.Message) map[string]any {
	switch message.Role {
	case ai.RoleTool:
		if message.Result == nil {
			return nil
		}
		return map[string]any{
			"role":      "tool",
			"tool_name": message.Result.Name,
			"content":   message.Result.Content,
		}
	case ai.RoleAssistant:
		out := map[string]any{"role": "assistant", "content": message.Content}
		if len(message.Calls) > 0 {
			out["tool_calls"] = calls(message.Calls)
		}
		return out
	default:
		return map[string]any{"role": string(message.Role), "content": message.Content}
	}
}

func calls(from []ai.ToolCall) []map[string]any {
	out := make([]map[string]any, 0, len(from))
	for _, call := range from {
		out = append(out, map[string]any{
			"function": map[string]any{"name": call.Name, "arguments": call.Arguments},
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
