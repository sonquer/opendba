package local

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/sonquer/opendba/src/cli/internal/ai"
)

// ParseCalls reads the tool calls out of a model's output and returns them with
// whatever text was not part of one.
func ParseCalls(output string) ([]ai.ToolCall, string, error) {
	var calls []ai.ToolCall
	var prose strings.Builder
	rest := output
	for {
		spoken, start := opening(rest)
		if start < 0 {
			prose.WriteString(rest)
			break
		}
		prose.WriteString(rest[:start])
		rest = rest[start+len(spoken.open):]
		end := strings.Index(rest, spoken.close)
		if end < 0 {
			return nil, "", fmt.Errorf("a tool call was started and never finished")
		}
		call, err := spoken.parse(rest[:end], len(calls))
		if err != nil {
			return nil, "", err
		}
		calls = append(calls, call)
		rest = rest[end+len(spoken.close):]
	}
	return calls, strings.TrimSpace(prose.String()), nil
}

// opening finds the first tool call in any dialect, and says which one it is.
func opening(text string) (dialect, int) {
	found, at := dialect{}, -1
	for _, spoken := range dialects() {
		start := strings.Index(text, spoken.open)
		if start < 0 {
			continue
		}
		if at < 0 || start < at || (start == at && len(spoken.open) > len(found.open)) {
			found, at = spoken, start
		}
	}
	return found, at
}

func decodeCall(body string, index int) (ai.ToolCall, error) {
	var shape struct {
		Name      string         `json:"name"`
		Arguments map[string]any `json:"arguments"`
	}
	if err := json.Unmarshal([]byte(body), &shape); err != nil {
		return ai.ToolCall{}, fmt.Errorf("read tool call: %w", err)
	}
	if shape.Name == "" {
		return ai.ToolCall{}, fmt.Errorf("a tool call has no name")
	}
	if shape.Arguments == nil {
		shape.Arguments = map[string]any{}
	}
	return ai.ToolCall{
		ID:        fmt.Sprintf("call_%d", index+1),
		Name:      shape.Name,
		Arguments: shape.Arguments,
	}, nil
}

// Validate checks a call against the tool it names. What it returns is written
// to be read by the model, because the model is what has to put it right.
func Validate(tool ai.Tool, call ai.ToolCall) error {
	for _, required := range tool.Parameters.Required {
		if _, ok := call.Arguments[required]; !ok {
			return fmt.Errorf("argument %q is required", required)
		}
	}
	for name, value := range call.Arguments {
		property, known := tool.Parameters.Properties[name]
		if !known {
			return fmt.Errorf("tool %s has no argument %q", tool.Name, name)
		}
		if err := matches(property, value); err != nil {
			return fmt.Errorf("argument %q %w", name, err)
		}
	}
	return nil
}

func matches(property ai.Property, value any) error {
	switch property.Type {
	case "string":
		text, ok := value.(string)
		if !ok {
			return fmt.Errorf("must be a string")
		}
		return within(property.Enum, text)
	case "integer", "number":
		if _, ok := value.(float64); !ok {
			return fmt.Errorf("must be a number")
		}
	case "boolean":
		if _, ok := value.(bool); !ok {
			return fmt.Errorf("must be true or false")
		}
	case "array":
		items, ok := value.([]any)
		if !ok {
			return fmt.Errorf("must be a list")
		}
		if property.Items == nil {
			return nil
		}
		for _, item := range items {
			if err := matches(*property.Items, item); err != nil {
				return err
			}
		}
	}
	return nil
}

func within(enum []string, text string) error {
	if len(enum) == 0 {
		return nil
	}
	for _, allowed := range enum {
		if allowed == text {
			return nil
		}
	}
	return fmt.Errorf("must be one of %s", strings.Join(enum, ", "))
}
