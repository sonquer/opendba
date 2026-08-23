package local

import (
	"context"
	"fmt"
	"io"
	"sync"

	"github.com/sonquer/tui4db/src/cli/internal/ai"
)

// Installed is a model on disk, with what the catalogue says about how to run it.
type Installed struct {
	ID          string
	Path        string
	Context     int
	MaxTokens   int
	Temperature float64
	TopP        float64
	TopK        int

	// Template is the chat format the catalogue names for this model, used
	// when the one in the file itself cannot be applied.
	Template string
}

// Models finds the models that have been downloaded.
type Models interface {
	Find(id string) (Installed, error)
}

// Provider opens models that run inside this process.
type Provider struct{ models Models }

// New returns a provider that loads from a set of installed models.
func New(models Models) *Provider { return &Provider{models: models} }

// Kind names this back-end.
func (p *Provider) Kind() ai.Kind { return ai.KindLocal }

// Open returns a client for an instance. The engine comes from the caller
// rather than from here, so that one loaded library serves every instance and a
// test can hand over one that loads nothing at all.
func (p *Provider) Open(instance ai.Instance, deps ai.Deps) (ai.Client, error) {
	if deps.Engine == nil {
		return nil, fmt.Errorf("local inference needs an engine")
	}
	if p.models == nil {
		return nil, fmt.Errorf("local inference needs somewhere to find models")
	}
	return &client{engine: deps.Engine, models: p.models, instance: instance}, nil
}

// client is one instance of a local model.
type client struct {
	engine   ai.Engine
	models   Models
	instance ai.Instance

	mu      sync.Mutex
	session ai.Session
	loaded  string
}

// Capabilities reports what a local model can do. Tools are answered with a
// grammar rather than with a provider's own tool protocol, which is why the
// grammar is advertised alongside them.
func (c *client) Capabilities() ai.Capabilities {
	return ai.Capabilities{
		Tools:     true,
		Streaming: true,
		Grammar:   true,
		Local:     true,
		Context:   c.instance.Context,
	}
}

// Probe reports whether this instance could answer: the library opens, and the
// model it names is on disk.
func (c *client) Probe(context.Context) error {
	if err := c.engine.Ready(); err != nil {
		return err
	}
	if _, err := c.models.Find(c.instance.Model); err != nil {
		return err
	}
	return nil
}

// Warm loads the model now rather than during the first question. The grammar
// is worked out from the same tools the conversation will use, because it is
// compiled into the sampler: warming without them would load the model once
// here and again on the first question.
func (c *client) Warm(ctx context.Context, tools []ai.Tool) error {
	grammar, err := grammarFor(tools)
	if err != nil {
		return err
	}
	_, err = c.sessionFor(ctx, c.instance.Model, grammar)
	return err
}

// Chat lays out the conversation, loads the model if it is not already loaded,
// and starts generating.
func (c *client) Chat(ctx context.Context, req ai.Request) (ai.Stream, error) {
	grammar, err := grammarFor(req.Tools)
	if err != nil {
		return nil, err
	}
	model := req.Model
	if model == "" {
		model = c.instance.Model
	}
	session, err := c.sessionFor(ctx, model, grammar)
	if err != nil {
		return nil, err
	}
	prompt, err := session.Template(req.Messages, req.System, req.Tools)
	if err != nil {
		return nil, err
	}
	if err := session.Reset(); err != nil {
		return nil, err
	}
	return start(ctx, session, prompt), nil
}

func grammarFor(tools []ai.Tool) (string, error) {
	if len(tools) == 0 {
		return "", nil
	}
	return Grammar(tools)
}

// sessionFor keeps one model loaded. The grammar is part of the sampler and so
// part of the load, which is why a change of tools reloads: it is rare, and the
// alternative is a sampler that no longer matches the tools on offer.
func (c *client) sessionFor(ctx context.Context, model, grammar string) (ai.Session, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	want := model + "\x00" + grammar
	if c.session != nil && c.loaded == want {
		return c.session, nil
	}
	installed, err := c.models.Find(model)
	if err != nil {
		return nil, err
	}
	if c.session != nil {
		if err := c.session.Close(); err != nil {
			return nil, fmt.Errorf("release the last model: %w", err)
		}
		c.session, c.loaded = nil, ""
	}
	opened, err := c.engine.Open(ctx, installed.Path, options(installed, c.instance, grammar))
	if err != nil {
		return nil, err
	}
	c.session, c.loaded = opened, want
	return opened, nil
}

func options(installed Installed, instance ai.Instance, grammar string) ai.EngineOptions {
	window := instance.Context
	if window <= 0 {
		window = installed.Context
	}
	return ai.EngineOptions{
		Template:    installed.Template,
		Context:     window,
		MaxTokens:   installed.MaxTokens,
		Temperature: installed.Temperature,
		TopP:        installed.TopP,
		TopK:        installed.TopK,
		Grammar:     grammar,
	}
}

// Close releases the loaded model.
func (c *client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.session == nil {
		return nil
	}
	err := c.session.Close()
	c.session, c.loaded = nil, ""
	return err
}

// stream turns the tokens a session produces into chunks. Generation runs in a
// goroutine because the loop is a blocking call into the library, and the
// screen has to stay answerable while it runs.
type stream struct {
	tokens  <-chan ai.Token
	failure <-chan error
	cancel  context.CancelFunc
	reader  reader
	pending []ai.Chunk
	ended   bool
}

func start(ctx context.Context, session ai.Session, prompt string) *stream {
	inner, cancel := context.WithCancel(ctx)
	tokens := make(chan ai.Token)
	failure := make(chan error, 1)
	go func() {
		defer close(tokens)
		failure <- session.Generate(inner, prompt, tokens)
	}()
	return &stream{tokens: tokens, failure: failure, cancel: cancel}
}

// Next returns the next chunk, or io.EOF when the answer is finished.
func (s *stream) Next() (ai.Chunk, error) {
	for {
		if len(s.pending) > 0 {
			chunk := s.pending[0]
			s.pending = s.pending[1:]
			return chunk, nil
		}
		if s.ended {
			return ai.Chunk{}, io.EOF
		}
		token, open := <-s.tokens
		if !open {
			return s.finish(ai.StopEndTurn)
		}
		if token.Err != nil {
			return ai.Chunk{}, token.Err
		}
		if token.Done {
			s.ended = true
			chunks, err := s.reader.done(token.Stop)
			if err != nil {
				return ai.Chunk{}, err
			}
			s.pending = chunks
			continue
		}
		s.pending = s.reader.add(token.Text)
		if s.reader.Ended() {
			s.cancel()
			return s.finish(ai.StopEndTurn)
		}
	}
}

// finish handles a loop that stopped without saying so, which is what a failure
// or a cancellation looks like from here.
func (s *stream) finish(stop ai.StopReason) (ai.Chunk, error) {
	s.ended = true
	if err := <-s.failure; err != nil {
		return ai.Chunk{}, err
	}
	chunks, err := s.reader.done(stop)
	if err != nil {
		return ai.Chunk{}, err
	}
	s.pending = chunks
	return s.Next()
}

// Close stops generation. A local model stops computing rather than being left
// to finish an answer nobody is going to read.
func (s *stream) Close() error {
	s.cancel()
	return nil
}
