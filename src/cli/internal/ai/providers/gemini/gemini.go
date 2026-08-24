// Package gemini speaks the generative language protocol.
package gemini

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/sonquer/tui4db/src/cli/internal/ai"
)

// DefaultEndpoint is where Google answers.
const DefaultEndpoint = "https://generativelanguage.googleapis.com/v1beta"

const defaultMaxTokens = 4096

// Provider opens clients that speak the generative language protocol.
type Provider struct{}

// New returns the provider.
func New() *Provider { return &Provider{} }

// Kind names this back-end.
func (p *Provider) Kind() ai.Kind { return ai.KindGemini }

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

// Probe asks for the model list, which costs nothing.
func (c *client) Probe(ctx context.Context) error {
	_, err := c.Models(ctx)
	return err
}

// Models reports what the endpoint is serving.
func (c *client) Models(ctx context.Context) ([]ai.ModelInfo, error) {
	response, err := c.transport.Do(ctx, func() (*http.Request, error) {
		return http.NewRequest(http.MethodGet, c.address("models", ""), nil)
	})
	if err != nil {
		return nil, err
	}
	defer func() { _ = response.Body.Close() }()

	var listed struct {
		Models []struct {
			Name        string `json:"name"`
			DisplayName string `json:"displayName"`
			InputLimit  int    `json:"inputTokenLimit"`
		} `json:"models"`
	}
	if err := json.NewDecoder(response.Body).Decode(&listed); err != nil {
		return nil, ai.Failure(ai.ReasonDecode, c.instance.Name, "read the model list", err)
	}
	models := make([]ai.ModelInfo, 0, len(listed.Models))
	for _, model := range listed.Models {
		models = append(models, ai.ModelInfo{
			ID:      strings.TrimPrefix(model.Name, "models/"),
			Title:   model.DisplayName,
			Context: model.InputLimit,
		})
	}
	return models, nil
}

// Chat starts an answer.
func (c *client) Chat(ctx context.Context, req ai.Request) (ai.Stream, error) {
	body, err := json.Marshal(c.body(req))
	if err != nil {
		return nil, ai.Failure(ai.ReasonRequest, c.instance.Name, "write the request", err)
	}
	model := req.Model
	if model == "" {
		model = c.instance.Model
	}
	address := c.address("models/"+model+":streamGenerateContent", "sse")
	response, err := c.transport.Do(ctx, func() (*http.Request, error) {
		request, err := http.NewRequest(http.MethodPost, address, bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Accept", "text/event-stream")
		return request, nil
	})
	if err != nil {
		return nil, err
	}
	return newStream(c.instance.Name, response.Body), nil
}

// address puts the key in the query string, which is where this protocol wants
// it.
func (c *client) address(path, alt string) string {
	query := url.Values{}
	if len(c.instance.Key) > 0 {
		query.Set("key", string(c.instance.Key))
	}
	if alt != "" {
		query.Set("alt", alt)
	}
	address := c.endpoint + "/" + path
	if len(query) == 0 {
		return address
	}
	return address + "?" + query.Encode()
}

func (c *client) body(req ai.Request) map[string]any {
	generation := map[string]any{"maxOutputTokens": defaultMaxTokens}
	if req.MaxTokens > 0 {
		generation["maxOutputTokens"] = req.MaxTokens
	}
	if req.Temperature > 0 {
		generation["temperature"] = req.Temperature
	}
	if req.TopP > 0 {
		generation["topP"] = req.TopP
	}
	if req.TopK > 0 {
		generation["topK"] = req.TopK
	}
	if len(req.Stop) > 0 {
		generation["stopSequences"] = req.Stop
	}
	body := map[string]any{
		"contents":         contents(req.Messages),
		"generationConfig": generation,
	}
	if system := strings.TrimSpace(req.System); system != "" {
		body["systemInstruction"] = map[string]any{"parts": []map[string]any{{"text": system}}}
	}
	if len(req.Tools) > 0 {
		body["tools"] = []map[string]any{{"functionDeclarations": declarations(req.Tools)}}
	}
	return body
}

// contents folds the conversation into the turns this protocol expects.
func contents(from []ai.Message) []map[string]any {
	out := make([]map[string]any, 0, len(from))
	for _, message := range from {
		parts, role := content(message)
		if len(parts) == 0 {
			continue
		}
		if last := len(out) - 1; last >= 0 && out[last]["role"] == role && role == "user" {
			out[last]["parts"] = append(out[last]["parts"].([]map[string]any), parts...)
			continue
		}
		out = append(out, map[string]any{"role": role, "parts": parts})
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
			"functionResponse": map[string]any{
				"name":     message.Result.Name,
				"response": map[string]any{"content": message.Result.Content, "failed": message.Result.Failed},
			},
		}}, "user"
	case ai.RoleAssistant:
		parts := make([]map[string]any, 0, len(message.Calls)+1)
		if text := strings.TrimSpace(message.Content); text != "" {
			parts = append(parts, map[string]any{"text": text})
		}
		for _, call := range message.Calls {
			parts = append(parts, map[string]any{
				"functionCall": map[string]any{"name": call.Name, "args": call.Arguments},
			})
		}
		return parts, "model"
	default:
		if text := strings.TrimSpace(message.Content); text != "" {
			return []map[string]any{{"text": text}}, "user"
		}
		return nil, "user"
	}
}

func declarations(from []ai.Tool) []map[string]any {
	out := make([]map[string]any, 0, len(from))
	for _, tool := range from {
		out = append(out, map[string]any{
			"name":        tool.Name,
			"description": tool.Description,
			"parameters":  tool.Parameters.JSON(),
		})
	}
	return out
}
