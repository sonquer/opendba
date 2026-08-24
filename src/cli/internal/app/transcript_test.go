package app

import (
	"reflect"
	"testing"

	"github.com/sonquer/tui4db/src/cli/internal/ai"
)

// A conversation that was kept draws exactly as the live one did. The same
// messages go through the live recorder and through the one that rebuilds a
// saved conversation, and the two have to agree — that is the whole point of
// replaying rather than reading.
func TestASavedConversationDrawsAsTheLiveOneDid(t *testing.T) {
	call := ai.ToolCall{
		ID:        "call-1",
		Name:      "run_select",
		Arguments: map[string]any{"sql": "SELECT count(*) FROM users", "limit": 10},
	}
	failed := ai.ToolResult{ID: "call-2", Name: "run_select", Content: "no such table", Failed: true}
	worked := ai.ToolResult{ID: "call-1", Name: "run_select", Content: "count\n2"}

	messages := []ai.Message{
		{Role: ai.RoleUser, Content: "how many users?"},
		{Role: ai.RoleAssistant, Reasoning: "counting is a select", Calls: []ai.ToolCall{call}},
		{Role: ai.RoleTool, Result: &worked},
		{Role: ai.RoleAssistant, Content: "There are two."},
		{Role: ai.RoleUser, Content: "and orders?"},
		{Role: ai.RoleAssistant, Calls: []ai.ToolCall{call}},
		{Role: ai.RoleTool, Result: &failed},
		{Role: ai.RoleAssistant, Content: "That table is not there."},
	}

	live := chat{}
	for _, message := range messages {
		if message.Role == ai.RoleUser {
			live.exchanges = append(live.exchanges, exchange{question: message.Content})
			continue
		}
		for _, event := range replayed(message) {
			live.record(event)
		}
	}
	for i := range live.exchanges {
		live.exchanges[i].done = true
	}

	kept := transcript(messages)
	if !reflect.DeepEqual(kept, live.exchanges) {
		t.Fatalf("a kept conversation reads differently:\nkept %+v\nlive %+v",
			kept, live.exchanges)
	}
	if len(kept) != 2 {
		t.Fatalf("exchanges = %d, want 2", len(kept))
	}
	if kept[0].question != "how many users?" || kept[0].answer != "There are two." {
		t.Errorf("first = %+v", kept[0])
	}
	if kept[0].reasoning != "counting is a select" {
		t.Errorf("reasoning = %q", kept[0].reasoning)
	}
	if len(kept[0].steps) != 1 {
		t.Errorf("steps = %v, a result that worked is not a step", kept[0].steps)
	}
	if len(kept[1].steps) != 2 || kept[1].steps[1] != "  no such table" {
		t.Errorf("steps = %v, a result that failed is", kept[1].steps)
	}
	for i, said := range kept {
		if !said.done {
			t.Errorf("exchange %d is not finished, and every kept one is", i)
		}
	}
}

// Messages that draw nothing draw nothing.
func TestMessagesThatDrawNothing(t *testing.T) {
	for _, want := range []struct {
		name    string
		message ai.Message
	}{
		{"a system prompt", ai.Message{Role: ai.RoleSystem, Content: "you are"}},
		{"a tool that returned nothing", ai.Message{Role: ai.RoleTool}},
		{"an assistant with nothing to say", ai.Message{Role: ai.RoleAssistant}},
	} {
		t.Run(want.name, func(t *testing.T) {
			if events := replayed(want.message); len(events) != 0 {
				t.Errorf("events = %+v, want none", events)
			}
		})
	}
	if said := transcript(nil); said != nil {
		t.Errorf("transcript = %+v, nothing said is nothing drawn", said)
	}
}

// An answer that arrived before anything was asked is dropped rather than
// drawn under a question that is not there.
func TestAnAnswerWithNoQuestionIsDropped(t *testing.T) {
	said := transcript([]ai.Message{
		{Role: ai.RoleAssistant, Content: "unprompted"},
		{Role: ai.RoleUser, Content: "hello"},
		{Role: ai.RoleAssistant, Content: "hello back"},
	})
	if len(said) != 1 {
		t.Fatalf("exchanges = %d, want 1", len(said))
	}
	if said[0].answer != "hello back" {
		t.Errorf("answer = %q", said[0].answer)
	}
}
