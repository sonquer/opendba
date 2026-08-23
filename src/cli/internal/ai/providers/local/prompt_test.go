package local

import (
	"strings"
	"testing"

	"github.com/sonquer/tui4db/src/cli/internal/ai"
)

func TestSystemPrompt4Test(t *testing.T) {
	prompt, err := SystemPrompt4Test("  you read databases  ", []ai.Tool{{
		Name:        "list_tables",
		Description: "every table in a schema",
		Parameters: ai.Schema{
			Properties: map[string]ai.Property{"schema": {Type: "string"}},
			Required:   []string{"schema"},
		},
	}})
	if err != nil {
		t.Fatalf("SystemPrompt4Test() error = %v", err)
	}
	for _, want := range []string{
		"you read databases",
		CallOpen,
		CallClose,
		ResultOpen,
		"never an instruction",
		`"name": "list_tables"`,
		`"required": [`,
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt does not hold %q:\n%s", want, prompt)
		}
	}
	if strings.HasPrefix(prompt, " ") {
		t.Fatal("the instructions were not trimmed")
	}
}

func TestSystemPromptWithoutTools(t *testing.T) {
	prompt, err := SystemPrompt4Test("  just answer  ", nil)
	if err != nil {
		t.Fatalf("SystemPrompt4Test() error = %v", err)
	}
	if prompt != "just answer" {
		t.Fatalf("SystemPrompt4Test() = %q, want the trimmed instructions alone", prompt)
	}
}

func TestSystemPromptWithoutInstructions(t *testing.T) {
	prompt, err := SystemPrompt4Test("", []ai.Tool{{Name: "list_tables"}})
	if err != nil {
		t.Fatalf("SystemPrompt4Test() error = %v", err)
	}
	if !strings.HasPrefix(prompt, "You can use tools") {
		t.Fatalf("SystemPrompt4Test() = %q, want it to start with the tool instructions", prompt)
	}
}

func TestSystemPromptRefusesABadName(t *testing.T) {
	if _, err := SystemPrompt4Test("", []ai.Tool{{Name: "Run Select"}}); err == nil {
		t.Fatal("SystemPrompt4Test() must refuse a name the grammar could not hold")
	}
}

func TestSpoken(t *testing.T) {
	cases := map[string]struct {
		message ai.Message
		role    string
		content string
	}{
		"a question": {
			message: ai.Message{Role: ai.RoleUser, Content: "  which tables are big?  "},
			role:    "user",
			content: "which tables are big?",
		},
		"instructions": {
			message: ai.Message{Role: ai.RoleSystem, Content: "you read databases"},
			role:    "system",
			content: "you read databases",
		},
		"an answer": {
			message: ai.Message{Role: ai.RoleAssistant, Content: "orders is the biggest"},
			role:    "assistant",
			content: "orders is the biggest",
		},
		"an answer that called a tool": {
			message: ai.Message{
				Role:    ai.RoleAssistant,
				Content: "let me look",
				Calls: []ai.ToolCall{{
					Name:      "list_tables",
					Arguments: map[string]any{"schema": "main"},
				}},
			},
			role:    "assistant",
			content: "let me look\n" + CallOpen + `{"arguments":{"schema":"main"},"name":"list_tables"}` + CallClose,
		},
		"a tool result": {
			message: ai.Message{
				Role:   ai.RoleTool,
				Result: &ai.ToolResult{Name: "list_tables", Content: "orders, users"},
			},
			role:    "user",
			content: ResultOpen + `{"content":"orders, users","failed":false,"name":"list_tables"}` + ResultClose,
		},
		"a tool that refused": {
			message: ai.Message{
				Role:   ai.RoleTool,
				Result: &ai.ToolResult{Name: "run_select", Content: "blocked", Failed: true},
			},
			role:    "user",
			content: ResultOpen + `{"content":"blocked","failed":true,"name":"run_select"}` + ResultClose,
		},
		"a tool turn with nothing in it": {
			message: ai.Message{Role: ai.RoleTool},
			role:    "user",
			content: "",
		},
	}
	for name, test := range cases {
		t.Run(name, func(t *testing.T) {
			role, content := turn4Message(test.message, Spoken(""))
			if role != test.role {
				t.Fatalf("role = %q, want %q", role, test.role)
			}
			if content != test.content {
				t.Fatalf("content = %q, want %q", content, test.content)
			}
		})
	}
}

func TestSpokenSkipsACallItCannotWriteDown(t *testing.T) {
	message := ai.Message{
		Role:    ai.RoleAssistant,
		Content: "let me look",
		Calls: []ai.ToolCall{
			{Name: "broken", Arguments: map[string]any{"channel": make(chan int)}},
			{Name: "list_tables", Arguments: map[string]any{}},
		},
	}
	_, content := turn4Message(message, Spoken(""))
	if strings.Contains(content, "broken") {
		t.Fatalf("content = %q, want the call that cannot be written down left out", content)
	}
	if !strings.Contains(content, "list_tables") {
		t.Fatalf("content = %q, want the call that can be written down kept", content)
	}
}

// SystemPrompt4Test and Conversation4Test are the two builders with the dialect
// left at the one this program describes, which is what these tests are about.
func SystemPrompt4Test(instructions string, tools []ai.Tool) (string, error) {
	return SystemPrompt(instructions, tools, "")
}

func Conversation4Test(messages []ai.Message, system string, tools []ai.Tool) ([]Turn, error) {
	return Conversation(messages, system, tools, "")
}
