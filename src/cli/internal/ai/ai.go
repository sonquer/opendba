package ai

// Kind names a back-end. A configured instance selects one, and the registry is
// keyed by it, so adding a back-end never means editing a switch somewhere else.
type Kind string

const (
	KindAnthropic  Kind = "anthropic"
	KindOpenAI     Kind = "openai"
	KindGemini     Kind = "gemini"
	KindOllama     Kind = "ollama"
	KindCompatible Kind = "compatible"
	KindLocal      Kind = "local"
)

// Role is who a message came from.
type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

// Message is one turn in a conversation. An assistant turn carries Calls when
// the model asked for tools, and a tool turn carries the Result of one of them.
type Message struct {
	Role      Role
	Content   string
	Reasoning string
	Calls     []ToolCall
	Result    *ToolResult
}

// ToolCall is the model asking for one tool to be run.
type ToolCall struct {
	ID        string
	Name      string
	Arguments map[string]any
}

// ToolResult is what a tool returned, on its way back to the model. Failed says
// the tool refused or errored, which the model is told about rather than being
// left to infer from the text.
type ToolResult struct {
	ID      string
	Name    string
	Content string
	Failed  bool
}

// Tool is one capability offered to the model.
type Tool struct {
	Name        string
	Description string
	Parameters  Schema
}

// Schema describes a tool's arguments. It is a deliberate subset of JSON Schema:
// what every provider accepts, and what a grammar can be generated from.
type Schema struct {
	Type       string
	Properties map[string]Property
	Required   []string
}

// Property is one field of a Schema.
type Property struct {
	Type        string
	Description string
	Enum        []string
	Items       *Property
}

// Request is one call to a model.
type Request struct {
	Model       string
	System      string
	Messages    []Message
	Tools       []Tool
	MaxTokens   int
	Temperature float64
	TopP        float64
	TopK        int
	Stop        []string
	Thinking    bool
}

// ChunkKind is what a piece of a stream is. Content blocks are bracketed by a
// start and an end rather than arriving as a flat run of text, because that is
// what lets a renderer know when a block is finished and can be laid out.
type ChunkKind string

const (
	ChunkTextStart      ChunkKind = "text_start"
	ChunkTextDelta      ChunkKind = "text_delta"
	ChunkTextEnd        ChunkKind = "text_end"
	ChunkReasoningStart ChunkKind = "reasoning_start"
	ChunkReasoningDelta ChunkKind = "reasoning_delta"
	ChunkReasoningEnd   ChunkKind = "reasoning_end"
	ChunkToolStart      ChunkKind = "tool_start"
	ChunkToolDelta      ChunkKind = "tool_delta"
	ChunkToolEnd        ChunkKind = "tool_end"
	ChunkDone           ChunkKind = "done"
)

// StopReason is why generation ended.
type StopReason string

const (
	StopEndTurn       StopReason = "end_turn"
	StopMaxTokens     StopReason = "max_tokens"
	StopToolUse       StopReason = "tool_use"
	StopCancelled     StopReason = "cancelled"
	StopContentFilter StopReason = "content_filter"
)

// Chunk is one piece of a stream.
type Chunk struct {
	Kind  ChunkKind
	Text  string
	Tool  *ToolCall
	Usage *Usage
	Stop  StopReason
}

// Usage is what a call cost. It holds both the inclusive totals every provider
// reports and a breakdown whose parts do not overlap, so a caller never has to
// subtract one from another and guess which convention was in play.
type Usage struct {
	Input          int
	Output         int
	Total          int
	NonCachedInput int
	CacheRead      int
	CacheWrite     int
	Reasoning      int
}

// VisibleOutput is the part of the output that was text rather than reasoning.
// It is the one place a subtraction happens, and it is clamped because a
// provider that reports more reasoning than output would otherwise produce a
// negative number.
func (u Usage) VisibleOutput() int { return max(u.Output-u.Reasoning, 0) }

// Balanced reports whether the disjoint parts of the input add up to the
// inclusive total, which is what a mapper is meant to guarantee.
func (u Usage) Balanced() bool {
	return u.NonCachedInput+u.CacheRead+u.CacheWrite == u.Input
}

// Capabilities is what an instance can do. Screens ask for these and degrade,
// exactly as they do with a database driver, rather than branching on a name.
type Capabilities struct {
	Tools     bool
	Streaming bool
	Reasoning bool
	Grammar   bool
	Local     bool
	Context   int
	MaxOutput int
}

// ModelInfo is one model an endpoint is serving.
type ModelInfo struct {
	ID      string
	Title   string
	Context int
}

// Instance is one configured way to reach a model, with the secret already
// resolved. It is what the registry opens a client from.
type Instance struct {
	Name     string
	Kind     Kind
	Model    string
	Endpoint string
	Key      []byte
	Context  int
	Thinking bool
}

// JSON renders the schema the way every provider spells it, which is JSON
// Schema. The shape is built here once rather than in each protocol, because
// the protocols disagree about the name of the field they put it in and about
// nothing else.
func (s Schema) JSON() map[string]any {
	kind := s.Type
	if kind == "" {
		kind = "object"
	}
	shape := map[string]any{"type": kind}
	if len(s.Properties) > 0 {
		properties := make(map[string]any, len(s.Properties))
		for name, property := range s.Properties {
			properties[name] = property.JSON()
		}
		shape["properties"] = properties
	}
	if len(s.Required) > 0 {
		shape["required"] = s.Required
	}
	return shape
}

// JSON renders one property of a schema.
func (p Property) JSON() map[string]any {
	shape := map[string]any{"type": p.Type}
	if p.Description != "" {
		shape["description"] = p.Description
	}
	if len(p.Enum) > 0 {
		shape["enum"] = p.Enum
	}
	if p.Items != nil {
		shape["items"] = p.Items.JSON()
	}
	return shape
}
