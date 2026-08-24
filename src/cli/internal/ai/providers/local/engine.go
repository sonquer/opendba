package local

import (
	"errors"

	"github.com/sonquer/opendba/src/cli/internal/ai"
)

// ErrNoLibrary is what an engine reports when the inference library is not
// where it was told to look.
var ErrNoLibrary = errors.New("the inference library is not installed")

const (
	minContext = 512

	// DefaultContext is the window a model is opened with when nobody chose one.
	DefaultContext = 8192

	// DefaultMaxTokens is how long an answer may be.
	DefaultMaxTokens = 2048

	// AllGPULayers asks llama.cpp to put every layer on the accelerator.
	AllGPULayers = 999
)

// Normalise fills in what a caller left at zero.
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
