// Package llama is the only code in this program that reaches into llama.cpp.
// Everything above it talks to the ai.Engine interface, so the native library
// is a detail of one package rather than a shape the whole program takes.
package llama

import (
	"context"
	"fmt"
	"sync"

	"github.com/hybridgroup/yzma/pkg/llama"

	"github.com/sonquer/tui4db/src/cli/internal/ai"
	"github.com/sonquer/tui4db/src/cli/internal/ai/providers/local"
)

const (
	pieceBuffer  = 256
	promptBuffer = 8 << 10
	samplerSeed  = 0x5eed
)

// Engine is llama.cpp running inside this process. The shared library is
// resolved and opened once, and every model afterwards is loaded through it.
type Engine struct {
	directory string
	once      sync.Once
	err       error
}

// New returns an engine that opens its library from a directory.
func New(directory string) *Engine { return &Engine{directory: directory} }

// Ready opens the library and starts the backend, once. Calling it again
// returns whatever the first call decided.
func (e *Engine) Ready() error {
	e.once.Do(func() {
		if err := llama.Load(e.directory); err != nil {
			e.err = fmt.Errorf("open the inference library in %s: %w: %w", e.directory, local.ErrNoLibrary, err)
			return
		}
		llama.LogSet(llama.LogSilent())
		llama.Init()
	})
	return e.err
}

// Devices reports the hardware the backend found and how much memory each part
// of it has. A device that will not say returns a negative number, because zero
// would mean measured and empty.
func (e *Engine) Devices() []ai.Device {
	if e.Ready() != nil {
		return nil
	}
	count := llama.GGMLBackendDeviceCount()
	devices := make([]ai.Device, 0, count)
	for index := range count {
		handle := llama.GGMLBackendDeviceGet(index)
		if handle == 0 {
			continue
		}
		free, total := llama.GGMLBackendDeviceMemory(handle)
		device := ai.Device{
			Name:        llama.GGMLBackendDeviceName(handle),
			Description: llama.GGMLBackendDeviceDescription(handle),
			Kind:        deviceKind(handle),
			FreeBytes:   -1,
			TotalBytes:  -1,
		}
		if total > 0 {
			device.FreeBytes, device.TotalBytes = int64(free), int64(total)
		}
		devices = append(devices, device)
	}
	return devices
}

func deviceKind(handle llama.GGMLBackendDevice) string {
	switch llama.GGMLBackendDevType(handle) {
	case llama.GGMLBackendDeviceTypeGPU:
		return "gpu"
	case llama.GGMLBackendDeviceTypeCPU:
		return "cpu"
	default:
		return "accelerator"
	}
}

// Open loads a model from a file and returns a session to generate with.
func (e *Engine) Open(_ context.Context, path string, opts ai.EngineOptions) (ai.Session, error) {
	if err := e.Ready(); err != nil {
		return nil, err
	}
	opts = local.Normalise(opts)

	modelParams := llama.ModelDefaultParams()
	modelParams.NGpuLayers = int32(opts.GPULayers)
	model, err := llama.ModelLoadFromFile(path, modelParams)
	if err != nil {
		return nil, fmt.Errorf("load %s: %w", path, err)
	}

	contextParams := llama.ContextDefaultParams()
	contextParams.NCtx = uint32(opts.Context)
	handle, err := llama.InitFromModel(model, contextParams)
	if err != nil {
		_ = llama.ModelFree(model)
		return nil, fmt.Errorf("start a context for %s: %w", path, err)
	}

	vocab := llama.ModelGetVocab(model)
	built := &session{
		model:     model,
		context:   handle,
		vocab:     vocab,
		sampler:   sampler(model, vocab, opts),
		template:  chatTemplate(model, opts.Template),
		maxTokens: opts.MaxTokens,
	}
	return built, nil
}

// chatTemplate picks a chat format this build of llama.cpp can actually apply.
//
// The one in the model file is preferred and often cannot be used: a modern
// file carries a Jinja template with macros and loops, and the applier here
// understands a fixed set of known families rather than Jinja, so it refuses
// the lot. Gemma 4 is eighteen kilobytes of exactly that. The catalogue names
// the family each model belongs to for this reason, and chatml is the last
// resort, being the format most fine-tunes are close to.
//
// Every candidate is tried rather than reasoned about, because the only thing
// that knows what this build understands is this build.
func chatTemplate(model llama.Model, named string) string {
	for _, candidate := range []string{llama.ModelChatTemplate(model, ""), named, fallbackTemplate} {
		if candidate != "" && applies(candidate) {
			return candidate
		}
	}
	return fallbackTemplate
}

// applies reports whether a template is one this build can lay a turn out with.
// The buffer is small on purpose: a template that needs more room says so with
// a length, and a template that cannot be applied at all says so with a
// negative number, which is the only answer being asked for here.
func applies(template string) bool {
	probe := []llama.ChatMessage{llama.NewChatMessage("user", "?")}
	return llama.ChatApplyTemplate(template, probe, true, make([]byte, probeBuffer)) >= 0
}

const (
	fallbackTemplate = "chatml"
	probeBuffer      = 256
)

// Close releases the backend. The library itself stays open, because a process
// that has unloaded it cannot load it again.
func (e *Engine) Close() error {
	if e.err != nil {
		return nil
	}
	llama.Close()
	return nil
}

// sampler builds the chain. When a grammar is given it goes first, so that the
// tokens the grammar forbids are gone before anything else has a say; a filter
// applied after the choice has been made would not be a constraint at all.
func sampler(model llama.Model, vocab llama.Vocab, opts ai.EngineOptions) llama.Sampler {
	if opts.Grammar == "" {
		params := llama.DefaultSamplerParams()
		params.Temp = float32(opts.Temperature)
		params.TopP = float32(opts.TopP)
		params.TopK = int32(opts.TopK)
		return llama.NewSampler(model, llama.DefaultSamplers, params)
	}
	chain := llama.SamplerChainInit(llama.SamplerChainDefaultParams())
	llama.SamplerChainAdd(chain, llama.SamplerInitGrammarLazyPatterns(
		vocab, opts.Grammar, "root", local.TriggerPatterns(), nil))
	llama.SamplerChainAdd(chain, llama.SamplerInitTopK(int32(opts.TopK)))
	llama.SamplerChainAdd(chain, llama.SamplerInitTopP(float32(opts.TopP), 1))
	llama.SamplerChainAdd(chain, llama.SamplerInitTempExt(float32(opts.Temperature), 0, 1))
	llama.SamplerChainAdd(chain, llama.SamplerInitDist(samplerSeed))
	return chain
}

// session is one loaded model with its context and sampler. llama.cpp allows
// one decode at a time on a context, so the lock is what makes the session
// usable from a screen that can ask again before the last answer is finished.
type session struct {
	mu        sync.Mutex
	model     llama.Model
	context   llama.Context
	vocab     llama.Vocab
	sampler   llama.Sampler
	template  string
	maxTokens int
}

// Template lays the conversation out the way the model was trained to read it,
// using the template carried in the model file rather than one of ours.
func (s *session) Template(messages []ai.Message, system string, tools []ai.Tool) (string, error) {
	turns, err := local.Conversation(messages, system, tools)
	if err != nil {
		return "", err
	}
	chat := make([]llama.ChatMessage, 0, len(turns))
	for _, turn := range turns {
		chat = append(chat, llama.NewChatMessage(turn.Role, turn.Content))
	}
	return apply(s.template, chat)
}

func apply(template string, chat []llama.ChatMessage) (string, error) {
	buf := make([]byte, promptBuffer)
	written := llama.ChatApplyTemplate(template, chat, true, buf)
	if written < 0 {
		return "", fmt.Errorf("apply the chat template")
	}
	if int(written) > len(buf) {
		buf = make([]byte, written)
		written = llama.ChatApplyTemplate(template, chat, true, buf)
		if written < 0 {
			return "", fmt.Errorf("apply the chat template")
		}
	}
	return string(buf[:written]), nil
}

// Generate runs the token loop, sending each piece of text as it is produced
// and stopping the moment the context is cancelled, so that a cancelled answer
// stops costing the machine anything rather than merely being ignored.
func (s *session) Generate(ctx context.Context, prompt string, out chan<- ai.Token) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	tokens := llama.Tokenize(s.vocab, prompt, true, true)
	if len(tokens) == 0 {
		return fmt.Errorf("the prompt produced no tokens")
	}
	batch := llama.BatchGetOne(tokens)
	var text local.Pieces
	stop := ai.StopMaxTokens

	for produced := 0; produced < s.maxTokens; produced++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		if _, err := llama.Decode(s.context, batch); err != nil {
			return fmt.Errorf("decode: %w", err)
		}
		token := llama.SamplerSample(s.sampler, s.context, -1)
		if llama.VocabIsEOG(s.vocab, token) {
			stop = ai.StopEndTurn
			break
		}
		if err := emit(ctx, out, text.Add(piece(s.vocab, token))); err != nil {
			return err
		}
		batch = llama.BatchGetOne([]llama.Token{token})
	}
	if err := emit(ctx, out, text.Flush()); err != nil {
		return err
	}
	return emitToken(ctx, out, ai.Token{Done: true, Stop: stop})
}

func piece(vocab llama.Vocab, token llama.Token) []byte {
	buf := make([]byte, pieceBuffer)
	written := llama.TokenToPiece(vocab, token, buf, 0, false)
	if written < 0 {
		buf = make([]byte, -written)
		written = llama.TokenToPiece(vocab, token, buf, 0, false)
	}
	if written <= 0 {
		return nil
	}
	return buf[:written]
}

func emit(ctx context.Context, out chan<- ai.Token, text string) error {
	if text == "" {
		return nil
	}
	return emitToken(ctx, out, ai.Token{Text: text})
}

func emitToken(ctx context.Context, out chan<- ai.Token, token ai.Token) error {
	select {
	case out <- token:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Reset empties the cache so that the next prompt is read from the beginning.
func (s *session) Reset() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	memory, err := llama.GetMemory(s.context)
	if err != nil {
		return fmt.Errorf("reach the cache: %w", err)
	}
	if err := llama.MemoryClear(memory, true); err != nil {
		return fmt.Errorf("clear the cache: %w", err)
	}
	return nil
}

// Close releases the model and everything held with it.
func (s *session) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	llama.SamplerFree(s.sampler)
	if err := llama.Free(s.context); err != nil {
		return fmt.Errorf("free the context: %w", err)
	}
	if err := llama.ModelFree(s.model); err != nil {
		return fmt.Errorf("free the model: %w", err)
	}
	return nil
}
