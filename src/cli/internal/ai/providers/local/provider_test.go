package local

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/sonquer/opendba/src/cli/internal/ai"
)

type fakeSession struct {
	tokens      []ai.Token
	generateErr error
	templateErr error
	resetErr    error
	closeErr    error

	prompt   string
	messages []ai.Message
	resets   int
	closes   int
	hold     chan struct{}
	stopped  chan struct{}
}

func (f *fakeSession) Generate(ctx context.Context, prompt string, out chan<- ai.Token) error {
	f.prompt = prompt
	for _, token := range f.tokens {
		select {
		case out <- token:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	if f.hold != nil {
		select {
		case <-f.hold:
		case <-ctx.Done():
			if f.stopped != nil {
				close(f.stopped)
			}
			return ctx.Err()
		}
	}
	return f.generateErr
}

func (f *fakeSession) Template(messages []ai.Message, _ string, _ []ai.Tool) (string, error) {
	f.messages = messages
	if f.templateErr != nil {
		return "", f.templateErr
	}
	return "<prompt>", nil
}

func (f *fakeSession) Reset() error {
	f.resets++
	return f.resetErr
}

func (f *fakeSession) Close() error {
	f.closes++
	return f.closeErr
}

type fakeEngine struct {
	ready    error
	openErr  error
	sessions []*fakeSession
	opened   []string
	options  []ai.EngineOptions
}

func (f *fakeEngine) Ready() error { return f.ready }

func (f *fakeEngine) Devices() []ai.Device { return nil }

func (f *fakeEngine) Open(_ context.Context, path string, opts ai.EngineOptions) (ai.Session, error) {
	f.opened = append(f.opened, path)
	f.options = append(f.options, opts)
	if f.openErr != nil {
		return nil, f.openErr
	}
	next := &fakeSession{}
	if len(f.sessions) > 0 {
		next = f.sessions[0]
		f.sessions = f.sessions[1:]
	}
	return next, nil
}

type fakeModels struct {
	entries map[string]Installed
}

func (f fakeModels) Find(id string) (Installed, error) {
	entry, ok := f.entries[id]
	if !ok {
		return Installed{}, fmt.Errorf("no model named %q is installed", id)
	}
	return entry, nil
}

func models() fakeModels {
	return fakeModels{entries: map[string]Installed{
		"gemma-4-e4b-qat": {
			ID:          "gemma-4-e4b-qat",
			Path:        "/models/gemma.gguf",
			Context:     8192,
			MaxTokens:   512,
			Temperature: 1,
			TopP:        0.95,
			TopK:        64,
		},
	}}
}

func opened(t *testing.T, engine ai.Engine) *client {
	t.Helper()
	provider := New(models())
	built, err := provider.Open(ai.Instance{Name: "local", Kind: ai.KindLocal, Model: "gemma-4-e4b-qat"}, ai.Deps{Engine: engine})
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	return built.(*client)
}

func collect(t *testing.T, stream ai.Stream) []ai.Chunk {
	t.Helper()
	var chunks []ai.Chunk
	for {
		chunk, err := stream.Next()
		if errors.Is(err, io.EOF) {
			return chunks
		}
		if err != nil {
			t.Fatalf("Next() error = %v", err)
		}
		chunks = append(chunks, chunk)
	}
}

func TestProviderKind(t *testing.T) {
	if got := New(models()).Kind(); got != ai.KindLocal {
		t.Fatalf("Kind() = %q, want local", got)
	}
}

func TestProviderNeedsItsParts(t *testing.T) {
	cases := map[string]struct {
		provider *Provider
		deps     ai.Deps
		want     string
	}{
		"no engine": {provider: New(models()), deps: ai.Deps{}, want: "needs an engine"},
		"no models": {provider: New(nil), deps: ai.Deps{Engine: &fakeEngine{}}, want: "find models"},
	}
	for name, test := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := test.provider.Open(ai.Instance{Kind: ai.KindLocal}, test.deps)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Open() error = %v, want it to mention %q", err, test.want)
			}
		})
	}
}

func TestClientCapabilities(t *testing.T) {
	client := opened(t, &fakeEngine{})
	client.instance.Context = 4096
	caps := client.Capabilities()
	if !caps.Tools || !caps.Streaming || !caps.Grammar || !caps.Local {
		t.Fatalf("Capabilities() = %+v, want a local streaming model with tools", caps)
	}
	if caps.Context != 4096 {
		t.Fatalf("Context = %d, want the instance's own", caps.Context)
	}
}

func TestClientProbe(t *testing.T) {
	broken := errors.New("no library")
	cases := map[string]struct {
		engine *fakeEngine
		model  string
		want   string
	}{
		"library missing": {engine: &fakeEngine{ready: broken}, model: "gemma-4-e4b-qat", want: "no library"},
		"model missing":   {engine: &fakeEngine{}, model: "qwen", want: "no model named"},
		"all present":     {engine: &fakeEngine{}, model: "gemma-4-e4b-qat"},
	}
	for name, test := range cases {
		t.Run(name, func(t *testing.T) {
			client := opened(t, test.engine)
			client.instance.Model = test.model
			err := client.Probe(context.Background())
			if test.want == "" {
				if err != nil {
					t.Fatalf("Probe() error = %v, want none", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Probe() error = %v, want it to mention %q", err, test.want)
			}
		})
	}
}

func TestClientChatStreamsText(t *testing.T) {
	session := &fakeSession{tokens: []ai.Token{
		{Text: "orders "},
		{Text: "is the biggest"},
		{Done: true, Stop: ai.StopEndTurn},
	}}
	engine := &fakeEngine{sessions: []*fakeSession{session}}
	client := opened(t, engine)

	stream, err := client.Chat(context.Background(), ai.Request{Messages: []ai.Message{{Role: ai.RoleUser, Content: "which?"}}})
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}
	chunks := collect(t, stream)
	if got := text(chunks); got != "orders is the biggest" {
		t.Fatalf("text = %q", got)
	}
	if chunks[len(chunks)-1].Stop != ai.StopEndTurn {
		t.Fatalf("stop = %q, want end_turn", chunks[len(chunks)-1].Stop)
	}
	if session.prompt != "<prompt>" {
		t.Fatalf("prompt = %q, want the templated conversation", session.prompt)
	}
	if session.resets != 1 {
		t.Fatalf("resets = %d, want the cache cleared once", session.resets)
	}
	if engine.options[0].Grammar != "" {
		t.Fatal("a question with no tools must not be constrained by a grammar")
	}
}

func TestClientChatWithTools(t *testing.T) {
	session := &fakeSession{tokens: []ai.Token{
		{Text: `<tool_call>{"name": "list_tables", "arguments": {}}</tool_call>`},
		{Done: true, Stop: ai.StopEndTurn},
	}}
	engine := &fakeEngine{sessions: []*fakeSession{session}}
	client := opened(t, engine)

	stream, err := client.Chat(context.Background(), ai.Request{Tools: []ai.Tool{{Name: "list_tables"}}})
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}
	chunks := collect(t, stream)
	if chunks[len(chunks)-1].Stop != ai.StopToolUse {
		t.Fatalf("stop = %q, want tool_use", chunks[len(chunks)-1].Stop)
	}
	if !strings.Contains(engine.options[0].Grammar, `name ::= "list_tables"`) {
		t.Fatalf("grammar = %q, want it to name the tool", engine.options[0].Grammar)
	}
	if engine.options[0].Context != 8192 || engine.options[0].TopK != 64 {
		t.Fatalf("options = %+v, want the catalogue's own", engine.options[0])
	}
}

func TestClientKeepsTheModelLoaded(t *testing.T) {
	first := &fakeSession{tokens: []ai.Token{{Done: true, Stop: ai.StopEndTurn}}}
	second := &fakeSession{tokens: []ai.Token{{Done: true, Stop: ai.StopEndTurn}}}
	engine := &fakeEngine{sessions: []*fakeSession{first, second}}
	client := opened(t, engine)

	for range 2 {
		stream, err := client.Chat(context.Background(), ai.Request{})
		if err != nil {
			t.Fatalf("Chat() error = %v", err)
		}
		collect(t, stream)
	}
	if len(engine.opened) != 1 {
		t.Fatalf("the model was loaded %d times, want once", len(engine.opened))
	}

	stream, err := client.Chat(context.Background(), ai.Request{Tools: []ai.Tool{{Name: "list_tables"}}})
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}
	collect(t, stream)
	if len(engine.opened) != 2 {
		t.Fatalf("the model was loaded %d times, want a reload when the tools changed", len(engine.opened))
	}
	if first.closes != 1 {
		t.Fatalf("the previous model was closed %d times, want once", first.closes)
	}
}

func TestClientChatFailures(t *testing.T) {
	broken := errors.New("no")
	cases := map[string]struct {
		engine  *fakeEngine
		request ai.Request
		want    string
	}{
		"a tool the grammar cannot hold": {
			engine:  &fakeEngine{},
			request: ai.Request{Tools: []ai.Tool{{Name: "Run"}}},
			want:    "lowercase",
		},
		"a model that is not installed": {
			engine:  &fakeEngine{},
			request: ai.Request{Model: "qwen"},
			want:    "no model named",
		},
		"a model that will not load": {
			engine:  &fakeEngine{openErr: broken},
			request: ai.Request{},
			want:    "no",
		},
		"a template that will not apply": {
			engine:  &fakeEngine{sessions: []*fakeSession{{templateErr: broken}}},
			request: ai.Request{},
			want:    "no",
		},
		"a cache that will not clear": {
			engine:  &fakeEngine{sessions: []*fakeSession{{resetErr: broken}}},
			request: ai.Request{},
			want:    "no",
		},
	}
	for name, test := range cases {
		t.Run(name, func(t *testing.T) {
			client := opened(t, test.engine)
			_, err := client.Chat(context.Background(), test.request)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Chat() error = %v, want it to mention %q", err, test.want)
			}
		})
	}
}

func TestClientReportsAFailedGeneration(t *testing.T) {
	broken := errors.New("decode failed")
	session := &fakeSession{generateErr: broken}
	client := opened(t, &fakeEngine{sessions: []*fakeSession{session}})

	stream, err := client.Chat(context.Background(), ai.Request{})
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}
	if _, err := stream.Next(); !errors.Is(err, broken) {
		t.Fatalf("Next() error = %v, want %v", err, broken)
	}
}

func TestClientReportsATokenThatFailed(t *testing.T) {
	broken := errors.New("sampler gave up")
	session := &fakeSession{tokens: []ai.Token{{Err: broken}}}
	client := opened(t, &fakeEngine{sessions: []*fakeSession{session}})

	stream, err := client.Chat(context.Background(), ai.Request{})
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}
	if _, err := stream.Next(); !errors.Is(err, broken) {
		t.Fatalf("Next() error = %v, want %v", err, broken)
	}
}

func TestClientEndsAnAnswerThatStoppedWithoutSayingSo(t *testing.T) {
	session := &fakeSession{tokens: []ai.Token{{Text: "half an ans"}}}
	client := opened(t, &fakeEngine{sessions: []*fakeSession{session}})

	stream, err := client.Chat(context.Background(), ai.Request{})
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}
	chunks := collect(t, stream)
	if got := text(chunks); got != "half an ans" {
		t.Fatalf("text = %q, want what was produced before the stream ended", got)
	}
	if chunks[len(chunks)-1].Kind != ai.ChunkDone {
		t.Fatalf("last chunk = %q, want the end", chunks[len(chunks)-1].Kind)
	}
}

func TestClientReportsABrokenCallAtTheEnd(t *testing.T) {
	session := &fakeSession{tokens: []ai.Token{
		{Text: `<tool_call>{"name":`},
		{Done: true, Stop: ai.StopEndTurn},
	}}
	client := opened(t, &fakeEngine{sessions: []*fakeSession{session}})

	stream, err := client.Chat(context.Background(), ai.Request{})
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}
	if _, err := stream.Next(); err == nil {
		t.Fatal("Next() must report the call that was never finished")
	}
}

func TestStreamCloseStopsGeneration(t *testing.T) {
	session := &fakeSession{
		tokens:  []ai.Token{{Text: "thinking"}},
		hold:    make(chan struct{}),
		stopped: make(chan struct{}),
	}
	client := opened(t, &fakeEngine{sessions: []*fakeSession{session}})

	stream, err := client.Chat(context.Background(), ai.Request{})
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}
	if _, err := stream.Next(); err != nil {
		t.Fatalf("Next() error = %v", err)
	}
	if err := stream.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	<-session.stopped
}

func TestClientClose(t *testing.T) {
	session := &fakeSession{tokens: []ai.Token{{Done: true, Stop: ai.StopEndTurn}}}
	client := opened(t, &fakeEngine{sessions: []*fakeSession{session}})

	if err := client.Close(); err != nil {
		t.Fatalf("Close() with nothing loaded error = %v", err)
	}
	stream, err := client.Chat(context.Background(), ai.Request{})
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}
	collect(t, stream)
	if err := client.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if session.closes != 1 {
		t.Fatalf("the model was closed %d times, want once", session.closes)
	}
}

func TestClientReportsAModelItCannotRelease(t *testing.T) {
	stuck := errors.New("still in use")
	first := &fakeSession{tokens: []ai.Token{{Done: true, Stop: ai.StopEndTurn}}, closeErr: stuck}
	client := opened(t, &fakeEngine{sessions: []*fakeSession{first, {}}})

	stream, err := client.Chat(context.Background(), ai.Request{})
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}
	collect(t, stream)

	_, err = client.Chat(context.Background(), ai.Request{Tools: []ai.Tool{{Name: "list_tables"}}})
	if !errors.Is(err, stuck) {
		t.Fatalf("Chat() error = %v, want it to wrap %v", err, stuck)
	}
}

func TestClientReportsABrokenCallWhenTheStreamJustEnds(t *testing.T) {
	session := &fakeSession{tokens: []ai.Token{{Text: `<tool_call>{"name":`}}}
	client := opened(t, &fakeEngine{sessions: []*fakeSession{session}})

	stream, err := client.Chat(context.Background(), ai.Request{})
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}
	if _, err := stream.Next(); err == nil {
		t.Fatal("Next() must report the call that was never finished")
	}
}

// TestWarmLoadsBeforeTheFirstQuestion is what the conversation screen calls
// when a model is chosen: the wait belongs where somebody can see it happening,
// not in the middle of the first answer.
func TestWarmLoadsBeforeTheFirstQuestion(t *testing.T) {
	engine := &fakeEngine{}
	c := opened(t, engine)
	tools := []ai.Tool{{Name: "tables", Description: "list the tables"}}
	if err := c.Warm(context.Background(), tools); err != nil {
		t.Fatalf("Warm() error = %v", err)
	}
	if len(engine.opened) != 1 {
		t.Fatalf("opened %d models, want the one that was warmed", len(engine.opened))
	}
	if engine.options[0].Grammar == "" {
		t.Fatal("the model was loaded without the grammar the tools need, so the first question loads it again")
	}

	if _, err := c.Chat(context.Background(), ai.Request{Tools: tools, Messages: []ai.Message{
		{Role: ai.RoleUser, Content: "which tables are there?"},
	}}); err != nil {
		t.Fatalf("Chat() error = %v", err)
	}
	if len(engine.opened) != 1 {
		t.Fatalf("opened %d models, want the warmed one used as it stands", len(engine.opened))
	}
}

func TestWarmReportsWhatItCannotLoad(t *testing.T) {
	c := opened(t, &fakeEngine{openErr: errors.New("the file is not a model")})
	if err := c.Warm(context.Background(), nil); err == nil {
		t.Fatal("Warm() said a model that would not open was ready")
	}
	broken := []ai.Tool{{Name: "one that cannot be named in a grammar\n"}}
	if err := c.Warm(context.Background(), broken); err == nil {
		t.Fatal("Warm() built a grammar out of a tool that has no name")
	}
}
