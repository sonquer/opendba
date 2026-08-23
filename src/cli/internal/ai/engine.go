package ai

import "context"

// Engine runs a model inside this process. The interface is declared here
// rather than beside its implementation because this package is what depends
// on it: the local back-end is one implementation, and a test fake is another.
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
	// Template names the chat format to fall back to when the one carried in
	// the model file cannot be applied. It is a name out of the catalogue
	// rather than a template of ours, because the shape of a turn belongs to
	// the model and guessing it wrong is a model that answers nonsense.
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
// reports. A device that cannot report its memory returns a negative number,
// the same convention the database drivers use, because zero means measured and
// empty.
type Device struct {
	Name        string
	Description string
	Kind        string
	FreeBytes   int64
	TotalBytes  int64
}
