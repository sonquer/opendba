// Package agent runs one conversation: it asks a model, runs the tools the
// model asks for, and asks again with what they returned, until the model has
// an answer or the round limit is reached.
package agent

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/sonquer/tui4db/src/cli/internal/ai"
)

// Tools is what a model may call.
type Tools interface {
	Definitions() []ai.Tool
	Call(ctx context.Context, call ai.ToolCall) ai.ToolResult
}

// Consent decides whether a turn may leave the machine. It is asked once per
// turn rather than once per request, and again whenever a turn would send a
// class of data that has not been approved yet.
type Consent interface {
	Allow(ctx context.Context, outbound Outbound) error
}

// Class is one kind of thing a request would carry.
type Class string

const (
	// ClassQuestion is what the person typed.
	ClassQuestion Class = "your question"
	// ClassSchema is the shape of the database: names, types, readings.
	ClassSchema Class = "the shape of the database"
	// ClassRows is data out of the tables themselves.
	ClassRows Class = "rows from the database"
)

// Outbound is what a turn would send, described by class rather than by byte,
// because that is the question a person can actually answer.
type Outbound struct {
	Instance string
	Model    string
	Classes  []Class
	Bytes    int
}

// EventKind is what happened.
type EventKind string

const (
	EventText      EventKind = "text"
	EventReasoning EventKind = "reasoning"
	EventCall      EventKind = "call"
	EventResult    EventKind = "result"
	EventDone      EventKind = "done"
)

// Event is what a caller sees while a turn runs.
type Event struct {
	Kind   EventKind
	Text   string
	Call   *ai.ToolCall
	Result *ai.ToolResult
	Usage  *ai.Usage
	Stop   ai.StopReason
}

// DefaultRounds is how many times a model may call tools and be asked again
// before the turn is stopped. A model that has not answered by then is looping.
const DefaultRounds = 8

const droppedResult = "a result that was here has been dropped to make room"

// Agent is one conversation with one instance.
type Agent struct {
	client   ai.Client
	tools    Tools
	consent  Consent
	instance ai.Instance
	system   string
	rounds   int
	messages []ai.Message
	approved map[Class]bool
}

// New starts a conversation.
func New(client ai.Client, tools Tools, consent Consent, instance ai.Instance, system string) *Agent {
	return &Agent{
		client:   client,
		tools:    tools,
		consent:  consent,
		instance: instance,
		system:   system,
		rounds:   DefaultRounds,
		approved: map[Class]bool{},
	}
}

// Messages is the conversation so far.
func (a *Agent) Messages() []ai.Message { return a.messages }

// Resume takes a conversation that was had earlier and carries on from it, so
// that a question asked after opening a saved conversation is answered with
// what was already said rather than from nothing.
//
// The messages are copied. A caller that keeps its own slice and appends to it
// must not find itself editing the conversation this is now having.
func (a *Agent) Resume(messages []ai.Message) {
	a.messages = append([]ai.Message(nil), messages...)
}

// Warm makes the back-end ready with the tools this conversation will use. A
// back-end with nothing to get ready says so by not offering to.
func (a *Agent) Warm(ctx context.Context) error {
	warmer, ok := a.client.(ai.Warmer)
	if !ok {
		return nil
	}
	return warmer.Warm(ctx, a.tools.Definitions())
}

// Close lets go of whatever the back-end is holding, which for a model running
// here is the memory it was loaded into.
func (a *Agent) Close() error {
	closer, ok := a.client.(io.Closer)
	if !ok {
		return nil
	}
	return closer.Close()
}

// Ask puts a question and runs the turn to its end, sending events as it goes.
// The channel is closed by the caller, not here: a caller that has stopped
// reading has cancelled the context, and this returns rather than blocking.
func (a *Agent) Ask(ctx context.Context, question string, out chan<- Event) error {
	if strings.TrimSpace(question) == "" {
		return fmt.Errorf("there is nothing to ask")
	}
	a.messages = append(a.messages, ai.Message{Role: ai.RoleUser, Content: question})
	return a.run(ctx, out)
}

func (a *Agent) run(ctx context.Context, out chan<- Event) error {
	for round := range a.rounds {
		if err := a.permit(ctx); err != nil {
			return err
		}
		answer, err := a.turn(ctx, out)
		if err != nil {
			return err
		}
		a.messages = append(a.messages, answer.message)
		if len(answer.message.Calls) == 0 {
			return send(ctx, out, Event{Kind: EventDone, Usage: answer.usage, Stop: answer.stop})
		}
		if err := a.work(ctx, answer.message.Calls, out); err != nil {
			return err
		}
		if round == a.rounds-1 {
			return send(ctx, out, Event{Kind: EventDone, Usage: answer.usage, Stop: ai.StopMaxTokens})
		}
	}
	return nil
}

// permit asks for consent when a turn would leave the machine, and only for the
// classes that have not been approved already.
func (a *Agent) permit(ctx context.Context) error {
	if a.consent == nil || a.client.Capabilities().Local {
		return nil
	}
	fresh := []Class{}
	for _, class := range a.classes() {
		if !a.approved[class] {
			fresh = append(fresh, class)
		}
	}
	if len(fresh) == 0 {
		return nil
	}
	err := a.consent.Allow(ctx, Outbound{
		Instance: a.instance.Name,
		Model:    a.instance.Model,
		Classes:  fresh,
		Bytes:    a.size(),
	})
	if err != nil {
		return err
	}
	for _, class := range fresh {
		a.approved[class] = true
	}
	return nil
}

// classes reads what the conversation is carrying. A tool that returned rows is
// what makes the difference between sending the shape of a database and sending
// what is in it.
func (a *Agent) classes() []Class {
	found := map[Class]bool{ClassQuestion: true}
	for _, message := range a.messages {
		if message.Result == nil {
			continue
		}
		if rowsFrom(message.Result.Name) {
			found[ClassRows] = true
			continue
		}
		found[ClassSchema] = true
	}
	ordered := []Class{}
	for _, class := range []Class{ClassQuestion, ClassSchema, ClassRows} {
		if found[class] {
			ordered = append(ordered, class)
		}
	}
	return ordered
}

func rowsFrom(tool string) bool { return tool == "run_select" }

func (a *Agent) size() int {
	total := len(a.system)
	for _, message := range a.messages {
		total += len(message.Content)
		if message.Result != nil {
			total += len(message.Result.Content)
		}
	}
	return total
}

type answer struct {
	message ai.Message
	usage   *ai.Usage
	stop    ai.StopReason
}

// turn is one request and the stream it produces. A conversation that no longer
// fits is compacted and tried again, because the alternative is telling someone
// their conversation is over.
func (a *Agent) turn(ctx context.Context, out chan<- Event) (answer, error) {
	for {
		built, err := a.stream(ctx, out)
		if err == nil {
			return built, nil
		}
		reason, classified := ai.ReasonOf(err)
		if !classified || reason != ai.ReasonContextOverflow || !a.compact() {
			return answer{}, err
		}
	}
}

func (a *Agent) stream(ctx context.Context, out chan<- Event) (answer, error) {
	stream, err := a.client.Chat(ctx, ai.Request{
		Model:     a.instance.Model,
		System:    a.system,
		Messages:  a.messages,
		Tools:     a.tools.Definitions(),
		Thinking:  a.instance.Thinking,
		MaxTokens: 0,
	})
	if err != nil {
		return answer{}, err
	}
	defer func() { _ = stream.Close() }()

	built := answer{message: ai.Message{Role: ai.RoleAssistant}, stop: ai.StopEndTurn}
	var text strings.Builder
	for {
		chunk, err := stream.Next()
		if errors.Is(err, io.EOF) {
			built.message.Content = text.String()
			return built, nil
		}
		if err != nil {
			return answer{}, err
		}
		switch chunk.Kind {
		case ai.ChunkTextDelta:
			text.WriteString(chunk.Text)
			if err := send(ctx, out, Event{Kind: EventText, Text: chunk.Text}); err != nil {
				return answer{}, err
			}
		case ai.ChunkReasoningDelta:
			built.message.Reasoning += chunk.Text
			if err := send(ctx, out, Event{Kind: EventReasoning, Text: chunk.Text}); err != nil {
				return answer{}, err
			}
		case ai.ChunkToolEnd:
			if chunk.Tool != nil {
				built.message.Calls = append(built.message.Calls, *chunk.Tool)
			}
		case ai.ChunkDone:
			built.usage, built.stop = chunk.Usage, chunk.Stop
		}
	}
}

// work runs the tools the model asked for, in the order it asked.
func (a *Agent) work(ctx context.Context, calls []ai.ToolCall, out chan<- Event) error {
	for i := range calls {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := send(ctx, out, Event{Kind: EventCall, Call: &calls[i]}); err != nil {
			return err
		}
		result := a.tools.Call(ctx, calls[i])
		a.messages = append(a.messages, ai.Message{Role: ai.RoleTool, Result: &result})
		if err := send(ctx, out, Event{Kind: EventResult, Result: &result}); err != nil {
			return err
		}
	}
	return nil
}

// compact makes room by emptying the oldest tool result. The message itself
// stays, because a provider that was told about a call and never told what it
// returned will refuse the whole conversation.
func (a *Agent) compact() bool {
	for i, message := range a.messages {
		if message.Result == nil || message.Result.Content == droppedResult {
			continue
		}
		dropped := *message.Result
		dropped.Content = droppedResult
		a.messages[i].Result = &dropped
		return true
	}
	return false
}

func send(ctx context.Context, out chan<- Event, event Event) error {
	select {
	case out <- event:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
