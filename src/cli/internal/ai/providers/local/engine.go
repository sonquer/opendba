package local

import (
	"errors"

	"github.com/sonquer/tui4db/src/cli/internal/ai"
)

// ErrNoLibrary is what an engine reports when the inference library is not
// where it was told to look.
var ErrNoLibrary = errors.New("the inference library is not installed")

const (
	minContext = 512

	// DefaultContext is the window a model is opened with when nobody chose
	// one. It is far short of what these models can hold, because the cache
	// costs memory per token and most questions about a database are short.
	DefaultContext = 8192

	// DefaultMaxTokens is how long an answer may be.
	DefaultMaxTokens = 2048

	// AllGPULayers asks llama.cpp to put every layer on the accelerator. It is
	// a count rather than a flag upstream, and any number past the layer count
	// of the model means all of them.
	AllGPULayers = 999
)

// Normalise fills in what a caller left at zero. Temperature, top-p and top-k
// are deliberately not touched: zero is a meaningful value for each of them,
// and the model card is what decides, not this package.
func Normalise(opts ai.EngineOptions) ai.EngineOptions {
	if opts.Context < minContext {
		opts.Context = DefaultContext
	}
	if opts.MaxTokens <= 0 {
		opts.MaxTokens = DefaultMaxTokens
	}
	if opts.GPULayers == 0 {
		opts.GPULayers = AllGPULayers
	}
	return opts
}
