package ai

import "context"

// Engine runs a model inside this process.
type Engine interface {
	Open(ctx context.Context, model string, opts EngineOptions) (Session, error)
	Ready() error
	Devices() []Device
}

// Session is one loaded model, with its context and sampler.
type Session interface {
	Generate(ctx context.Context, prompt string, out chan<- Token) error
	Template(messages []Message, system string, tools []Tool) (string, error)
	Reset() error
	Close() error
}

// EngineOptions is how a model is loaded and sampled.
type EngineOptions struct {
	// Template names the chat format to fall back to when the one carried in the
	// model file cannot be applied.
	Template string

	Context     int
	GPULayers   int
	Temperature float64
	TopP        float64
	TopK        int
	Grammar     string
	MaxTokens   int
}

// Token is one step of generation. Done marks the last one, and carries the
// reason generation stopped.
type Token struct {
	Text string
	Done bool
	Stop StopReason
	Err  error
}

// Device is one piece of hardware the engine can run on, and how much memory it
// reports.
type Device struct {
	Name        string
	Description string
	Kind        string
	FreeBytes   int64
	TotalBytes  int64
}
