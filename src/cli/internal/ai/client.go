package ai

import (
	"context"
	"net/http"
)

// Client is one configured way to reach a model.
type Client interface {
	Chat(ctx context.Context, req Request) (Stream, error)
	Capabilities() Capabilities
}

// Stream yields the chunks of one answer. Next returns io.EOF when the answer
// is finished, and any other error ends the stream just as finally.
type Stream interface {
	Next() (Chunk, error)
	Close() error
}

// Warmer is a client that can be made ready before it is asked anything.
//
// It exists for the local back-end, where being ready means reading gigabytes
// off a disk into memory. Doing that during the first question makes the
// program look hung; doing it when the model is chosen makes it a thing that is
// visibly happening. The tools are part of it because a grammar is compiled
// into the sampler, and a model made ready without them would be loaded twice.
type Warmer interface {
	Warm(ctx context.Context, tools []Tool) error
}

// Prober answers whether an instance is usable, which is what the test action
// on the settings screen asks. A back-end that cannot be probed without
// spending tokens does not implement it.
type Prober interface {
	Probe(ctx context.Context) error
}

// Lister reports the models an endpoint is serving. Only the back-ends that can
// enumerate implement it; the rest are configured with a model name.
type Lister interface {
	Models(ctx context.Context) ([]ModelInfo, error)
}

// Doer is the part of an HTTP client a back-end uses. Tests replace it, which
// is why nothing constructs http.DefaultClient for itself.
type Doer interface {
	Do(req *http.Request) (*http.Response, error)
}

// Deps is what a back-end needs and must not build for itself.
type Deps struct {
	HTTP   Doer
	Engine Engine
}

// Provider opens clients for one kind of back-end. Adding a back-end is a
// package that implements this and a line that registers it.
type Provider interface {
	Kind() Kind
	Open(instance Instance, deps Deps) (Client, error)
}
