package local

import (
	"strings"
	"testing"

	"github.com/sonquer/opendba/src/cli/internal/ai"
)

func TestParseCalls(t *testing.T) {
	cases := map[string]struct {
		output string
		calls  []ai.ToolCall
		prose  string
	}{
		"prose only": {
			output: "the orders table has no primary key",
			prose:  "the orders table has no primary key",
		},
		"one call": {
			output: `<tool_call>{"name": "list_tables", "arguments": {"schema": "main"}}</tool_call>`,
			calls: []ai.ToolCall{{
				ID:        "call_1",
				Name:      "list_tables",
				Arguments: map[string]any{"schema": "main"},
			}},
		},
		"prose then a call": {
			output: `let me look. <tool_call>{"name": "list_tables", "arguments": {}}</tool_call>`,
			calls:  []ai.ToolCall{{ID: "call_1", Name: "list_tables", Arguments: map[string]any{}}},
			prose:  "let me look.",
		},
		"two calls": {
			output: `<tool_call>{"name": "a", "arguments": {}}</tool_call><tool_call>{"name": "b", "arguments": {}}</tool_call>`,
			calls: []ai.ToolCall{
				{ID: "call_1", Name: "a", Arguments: map[string]any{}},
				{ID: "call_2", Name: "b", Arguments: map[string]any{}},
			},
		},
		"missing arguments become empty": {
			output: `<tool_call>{"name": "a"}</tool_call>`,
			calls:  []ai.ToolCall{{ID: "call_1", Name: "a", Arguments: map[string]any{}}},
		},
		"text after a call is kept": {
			output: `<tool_call>{"name": "a", "arguments": {}}</tool_call> done`,
			calls:  []ai.ToolCall{{ID: "call_1", Name: "a", Arguments: map[string]any{}}},
			prose:  "done",
		},
	}
	for name, test := range cases {
		t.Run(name, func(t *testing.T) {
			calls, prose, err := ParseCalls(test.output)
			if err != nil {
				t.Fatalf("ParseCalls() error = %v", err)
			}
			if prose != test.prose {
				t.Fatalf("prose = %q, want %q", prose, test.prose)
			}
			if len(calls) != len(test.calls) {
				t.Fatalf("read %d calls, want %d", len(calls), len(test.calls))
			}
			for i, want := range test.calls {
				if calls[i].ID != want.ID || calls[i].Name != want.Name {
					t.Fatalf("call %d = %+v, want %+v", i, calls[i], want)
				}
				if len(calls[i].Arguments) != len(want.Arguments) {
					t.Fatalf("call %d arguments = %v, want %v", i, calls[i].Arguments, want.Arguments)
				}
				for key, value := range want.Arguments {
					if calls[i].Arguments[key] != value {
						t.Fatalf("call %d argument %q = %v, want %v", i, key, calls[i].Arguments[key], value)
					}
				}
			}
		})
	}
}

func TestParseCallsRefusesRubbish(t *testing.T) {
	cases := map[string]struct {
		output string
		want   string
	}{
		"never closed": {
			output: `<tool_call>{"name": "a"}`,
			want:   "never finished",
		},
		"not json": {
			output: `<tool_call>name is a</tool_call>`,
			want:   "read tool call",
		},
		"no name": {
			output: `<tool_call>{"arguments": {}}</tool_call>`,
			want:   "no name",
		},
	}
	for name, test := range cases {
		t.Run(name, func(t *testing.T) {
			_, _, err := ParseCalls(test.output)
			if err == nil {
				t.Fatal("ParseCalls() must refuse this")
			}
			if !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %q, want it to mention %q", err, test.want)
			}
		})
	}
}

func tool() ai.Tool {
	return ai.Tool{
		Name: "run_select",
		Parameters: ai.Schema{
			Type: "object",
			Properties: map[string]ai.Property{
				"statement": {Type: "string"},
				"limit":     {Type: "integer"},
				"explain":   {Type: "boolean"},
				"schemas":   {Type: "array", Items: &ai.Property{Type: "string"}},
				"mode":      {Type: "string", Enum: []string{"read", "plan"}},
				"anything":  {Type: "object"},
				"loose":     {Type: "array"},
			},
			Required: []string{"statement"},
		},
	}
}

func TestValidate(t *testing.T) {
	cases := map[string]struct {
		arguments map[string]any
		want      string
	}{
		"the required argument": {
			arguments: map[string]any{"statement": "select 1"},
		},
		"every type": {
			arguments: map[string]any{
				"statement": "select 1",
				"limit":     float64(10),
				"explain":   true,
				"schemas":   []any{"main"},
				"mode":      "read",
				"anything":  map[string]any{"a": 1},
			},
		},
		"a list with no declared item type": {
			arguments: map[string]any{"statement": "select 1", "loose": []any{float64(1), "two"}},
		},
		"required missing": {
			arguments: map[string]any{"limit": float64(10)},
			want:      `argument "statement" is required`,
		},
		"unknown argument": {
			arguments: map[string]any{"statement": "select 1", "table": "orders"},
			want:      `has no argument "table"`,
		},
		"wrong string": {
			arguments: map[string]any{"statement": float64(1)},
			want:      "must be a string",
		},
		"wrong number": {
			arguments: map[string]any{"statement": "select 1", "limit": "ten"},
			want:      "must be a number",
		},
		"wrong boolean": {
			arguments: map[string]any{"statement": "select 1", "explain": "yes"},
			want:      "must be true or false",
		},
		"wrong list": {
			arguments: map[string]any{"statement": "select 1", "schemas": "main"},
			want:      "must be a list",
		},
		"wrong item in a list": {
			arguments: map[string]any{"statement": "select 1", "schemas": []any{float64(1)}},
			want:      "must be a string",
		},
		"outside the enum": {
			arguments: map[string]any{"statement": "select 1", "mode": "write"},
			want:      "must be one of read, plan",
		},
	}
	for name, test := range cases {
		t.Run(name, func(t *testing.T) {
			err := Validate(tool(), ai.ToolCall{Name: "run_select", Arguments: test.arguments})
			if test.want == "" {
				if err != nil {
					t.Fatalf("Validate() error = %v, want none", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("Validate() accepted %v", test.arguments)
			}
			if !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %q, want it to mention %q", err, test.want)
			}
		})
	}
}
