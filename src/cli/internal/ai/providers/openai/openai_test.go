package openai

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/sonquer/opendba/src/cli/internal/ai"
)

type recorded struct {
	status   int
	body     string
	header   http.Header
	requests []*http.Request
	bodies   []string
	err      error
}

func (r *recorded) Do(request *http.Request) (*http.Response, error) {
	r.requests = append(r.requests, request)
	if request.Body != nil {
		read, _ := io.ReadAll(request.Body)
		r.bodies = append(r.bodies, string(read))
	}
	if r.err != nil {
		return nil, r.err
	}
	header := r.header
	if header == nil {
		header = http.Header{}
	}
	status := r.status
	if status == 0 {
		status = http.StatusOK
	}
	return &http.Response{
		StatusCode: status,
		Body:       io.NopCloser(strings.NewReader(r.body)),
		Header:     header,
	}, nil
}

func cassette(t *testing.T, name string) string {
	t.Helper()
	read, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatalf("read cassette: %v", err)
	}
	return string(read)
}

func client4Test(t *testing.T, doer ai.Doer, instance ai.Instance) ai.Client {
	t.Helper()
	if instance.Name == "" {
		instance.Name = "work"
	}
	if instance.Kind == "" {
		instance.Kind = ai.KindOpenAI
	}
	built, err := New().Open(instance, ai.Deps{HTTP: doer})
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	built.(*client).transport.Sleep = func(context.Context, time.Duration) error { return nil }
	return built
}

func drain(t *testing.T, stream ai.Stream) []ai.Chunk {
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

func spoken(chunks []ai.Chunk, kind ai.ChunkKind) string {
	var built strings.Builder
	for _, chunk := range chunks {
		if chunk.Kind == kind {
			built.WriteString(chunk.Text)
		}
	}
	return built.String()
}

func TestChatStreamsText(t *testing.T) {
	doer := &recorded{body: cassette(t, "text.sse")}
	client := client4Test(t, doer, ai.Instance{Model: "gpt-5", Key: []byte("sk-test")})

	stream, err := client.Chat(context.Background(), ai.Request{
		System:   "you read databases",
		Messages: []ai.Message{{Role: ai.RoleUser, Content: "which table is biggest?"}},
	})
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}
	chunks := drain(t, stream)

	if got := spoken(chunks, ai.ChunkTextDelta); got != "The orders table is the biggest." {
		t.Fatalf("text = %q", got)
	}
	last := chunks[len(chunks)-1]
	if last.Kind != ai.ChunkDone || last.Stop != ai.StopEndTurn {
		t.Fatalf("last chunk = %+v, want a clean end", last)
	}
	if last.Usage == nil {
		t.Fatal("no usage was reported")
	}
	if !last.Usage.Balanced() {
		t.Fatalf("usage = %+v, want the parts of the input to add up", last.Usage)
	}
	if last.Usage.VisibleOutput() != 12 {
		t.Fatalf("visible output = %d, want the reasoning taken off", last.Usage.VisibleOutput())
	}
}

func TestChatWritesTheRequest(t *testing.T) {
	doer := &recorded{body: cassette(t, "text.sse")}
	client := client4Test(t, doer, ai.Instance{Model: "gpt-5", Key: []byte("sk-test")})

	_, err := client.Chat(context.Background(), ai.Request{
		System: "you read databases",
		Messages: []ai.Message{
			{Role: ai.RoleUser, Content: "which?"},
			{Role: ai.RoleAssistant, Calls: []ai.ToolCall{{ID: "call_1", Name: "list_tables", Arguments: map[string]any{"schema": "main"}}}},
			{Role: ai.RoleTool, Result: &ai.ToolResult{ID: "call_1", Name: "list_tables", Content: "orders"}},
		},
		Tools:       []ai.Tool{{Name: "list_tables", Description: "every table"}},
		Temperature: 0.4,
		TopP:        0.9,
		Stop:        []string{"###"},
	})
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}

	var body map[string]any
	if err := json.Unmarshal([]byte(doer.bodies[0]), &body); err != nil {
		t.Fatalf("the request was not json: %v", err)
	}
	if body["model"] != "gpt-5" || body["stream"] != true {
		t.Fatalf("body = %v, want a streamed request for the instance's model", body)
	}
	messages := body["messages"].([]any)
	if len(messages) != 4 {
		t.Fatalf("sent %d messages, want the system message and three turns", len(messages))
	}
	first := messages[0].(map[string]any)
	if first["role"] != "system" || first["content"] != "you read databases" {
		t.Fatalf("first message = %v, want the system message", first)
	}
	third := messages[2].(map[string]any)
	if _, ok := third["tool_calls"]; !ok {
		t.Fatal("the assistant turn lost its tool call")
	}
	fourth := messages[3].(map[string]any)
	if fourth["role"] != "tool" || fourth["tool_call_id"] != "call_1" {
		t.Fatalf("fourth message = %v, want the tool result", fourth)
	}
	if _, ok := body["tools"]; !ok {
		t.Fatal("the tools were not sent")
	}
	if request := doer.requests[0]; request.Header.Get("Authorization") != "Bearer sk-test" {
		t.Fatalf("authorization = %q", request.Header.Get("Authorization"))
	}
	if got := doer.requests[0].URL.String(); got != DefaultEndpoint+"/chat/completions" {
		t.Fatalf("url = %q", got)
	}
}

func TestChatGathersAToolCall(t *testing.T) {
	doer := &recorded{body: cassette(t, "tools.sse")}
	client := client4Test(t, doer, ai.Instance{Model: "gpt-5"})

	stream, err := client.Chat(context.Background(), ai.Request{Tools: []ai.Tool{{Name: "describe_table"}}})
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}
	chunks := drain(t, stream)

	if got := spoken(chunks, ai.ChunkTextDelta); got != "let me look" {
		t.Fatalf("text = %q", got)
	}
	var call *ai.ToolCall
	for _, chunk := range chunks {
		if chunk.Kind == ai.ChunkToolEnd {
			call = chunk.Tool
		}
	}
	if call == nil {
		t.Fatal("no tool call was produced")
	}
	if call.ID != "call_abc" || call.Name != "describe_table" {
		t.Fatalf("call = %+v", call)
	}
	if call.Arguments["schema"] != "main" || call.Arguments["table"] != "orders" {
		t.Fatalf("arguments = %v, want them gathered from every fragment", call.Arguments)
	}
	if chunks[len(chunks)-1].Stop != ai.StopToolUse {
		t.Fatalf("stop = %q, want tool_use", chunks[len(chunks)-1].Stop)
	}
	if got := spoken(chunks, ai.ChunkToolDelta); got != `{"schema": "main", "table": "orders"}` {
		t.Fatalf("tool deltas = %q, want every fragment forwarded", got)
	}
}

func TestChatSeparatesReasoning(t *testing.T) {
	doer := &recorded{body: cassette(t, "reasoning.sse")}
	client := client4Test(t, doer, ai.Instance{Model: "gpt-5"})

	stream, err := client.Chat(context.Background(), ai.Request{})
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}
	chunks := drain(t, stream)

	if got := spoken(chunks, ai.ChunkReasoningDelta); got != "the user wants the largest table" {
		t.Fatalf("reasoning = %q", got)
	}
	if got := spoken(chunks, ai.ChunkTextDelta); got != "orders" {
		t.Fatalf("text = %q", got)
	}
	if chunks[len(chunks)-1].Stop != ai.StopMaxTokens {
		t.Fatalf("stop = %q, want max_tokens", chunks[len(chunks)-1].Stop)
	}
	order := []ai.ChunkKind{}
	for _, chunk := range chunks {
		order = append(order, chunk.Kind)
	}
	want := []ai.ChunkKind{
		ai.ChunkReasoningStart, ai.ChunkReasoningDelta, ai.ChunkReasoningEnd,
		ai.ChunkTextStart, ai.ChunkTextDelta, ai.ChunkTextEnd, ai.ChunkDone,
	}
	if len(order) != len(want) {
		t.Fatalf("chunks = %v, want %v", order, want)
	}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("chunk %d = %q, want %q", i, order[i], want[i])
		}
	}
}

func TestChatReportsAnErrorInTheStream(t *testing.T) {
	doer := &recorded{body: cassette(t, "error.sse")}
	client := client4Test(t, doer, ai.Instance{Model: "gpt-5"})

	stream, err := client.Chat(context.Background(), ai.Request{})
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}
	_, err = stream.Next()
	reason, ok := ai.ReasonOf(err)
	if !ok || reason != ai.ReasonContextOverflow {
		t.Fatalf("reason = %q, want the overflow read out of the message", reason)
	}
}

func TestChatReportsAFrameItCannotRead(t *testing.T) {
	doer := &recorded{body: "data: {not json}\n\n"}
	client := client4Test(t, doer, ai.Instance{Model: "gpt-5"})

	stream, err := client.Chat(context.Background(), ai.Request{})
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}
	_, err = stream.Next()
	if reason, _ := ai.ReasonOf(err); reason != ai.ReasonDecode {
		t.Fatalf("reason = %q, want decode", reason)
	}
}

func TestChatSurvivesArgumentsItCannotRead(t *testing.T) {
	doer := &recorded{body: `data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","function":{"name":"a","arguments":"{oops"}}]},"finish_reason":"tool_calls"}]}` + "\n\n"}
	client := client4Test(t, doer, ai.Instance{Model: "gpt-5"})

	stream, err := client.Chat(context.Background(), ai.Request{})
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}
	chunks := drain(t, stream)
	for _, chunk := range chunks {
		if chunk.Kind == ai.ChunkToolEnd {
			if chunk.Tool.Name != "a" || len(chunk.Tool.Arguments) != 0 {
				t.Fatalf("call = %+v, want the name kept and the arguments empty", chunk.Tool)
			}
			return
		}
	}
	t.Fatal("the call was lost entirely")
}

func TestChatNamesACallTheProviderDidNot(t *testing.T) {
	doer := &recorded{body: `data: {"choices":[{"delta":{"tool_calls":[{"index":2,"function":{"name":"a","arguments":"{}"}}]},"finish_reason":"tool_calls"}]}` + "\n\n"}
	client := client4Test(t, doer, ai.Instance{Model: "gpt-5"})

	stream, err := client.Chat(context.Background(), ai.Request{})
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}
	for _, chunk := range drain(t, stream) {
		if chunk.Kind == ai.ChunkToolEnd && chunk.Tool.ID != "call_3" {
			t.Fatalf("id = %q, want one made from the index", chunk.Tool.ID)
		}
	}
}

func TestModels(t *testing.T) {
	doer := &recorded{body: `{"data":[{"id":"gpt-5"},{"id":"gpt-5-mini"}]}`}
	client := client4Test(t, doer, ai.Instance{Model: "gpt-5"})

	listed, err := client.(ai.Lister).Models(context.Background())
	if err != nil {
		t.Fatalf("Models() error = %v", err)
	}
	if len(listed) != 2 || listed[0].ID != "gpt-5" {
		t.Fatalf("Models() = %+v", listed)
	}
	if err := client.(ai.Prober).Probe(context.Background()); err != nil {
		t.Fatalf("Probe() error = %v", err)
	}
	if got := doer.requests[0].URL.Path; got != "/v1/models" {
		t.Fatalf("path = %q", got)
	}
}

func TestModelsReportsRubbish(t *testing.T) {
	doer := &recorded{body: "not json"}
	client := client4Test(t, doer, ai.Instance{Model: "gpt-5"})

	_, err := client.(ai.Lister).Models(context.Background())
	if reason, _ := ai.ReasonOf(err); reason != ai.ReasonDecode {
		t.Fatalf("reason = %q, want decode", reason)
	}
}

func TestProbeReportsARefusal(t *testing.T) {
	doer := &recorded{status: http.StatusUnauthorized, body: `{"error":{"message":"bad key"}}`}
	client := client4Test(t, doer, ai.Instance{Model: "gpt-5"})

	err := client.(ai.Prober).Probe(context.Background())
	if reason, _ := ai.ReasonOf(err); reason != ai.ReasonAuth {
		t.Fatalf("reason = %q, want auth", reason)
	}
}

func TestOpen(t *testing.T) {
	cases := map[string]struct {
		provider *Provider
		instance ai.Instance
		deps     ai.Deps
		endpoint string
		err      string
	}{
		"openai defaults its endpoint": {
			provider: New(),
			instance: ai.Instance{Name: "work"},
			deps:     ai.Deps{HTTP: &recorded{}},
			endpoint: DefaultEndpoint,
		},
		"an endpoint of one's own wins": {
			provider: New(),
			instance: ai.Instance{Name: "work", Endpoint: "https://llm.internal/v1/"},
			deps:     ai.Deps{HTTP: &recorded{}},
			endpoint: "https://llm.internal/v1",
		},
		"a compatible instance needs one": {
			provider: Compatible(),
			instance: ai.Instance{Name: "work"},
			deps:     ai.Deps{HTTP: &recorded{}},
			err:      "needs an endpoint",
		},
		"no http client": {
			provider: New(),
			instance: ai.Instance{Name: "work"},
			deps:     ai.Deps{},
			err:      "needs an http client",
		},
	}
	for name, test := range cases {
		t.Run(name, func(t *testing.T) {
			built, err := test.provider.Open(test.instance, test.deps)
			if test.err != "" {
				if err == nil || !strings.Contains(err.Error(), test.err) {
					t.Fatalf("Open() error = %v, want it to mention %q", err, test.err)
				}
				return
			}
			if err != nil {
				t.Fatalf("Open() error = %v", err)
			}
			if got := built.(*client).endpoint; got != test.endpoint {
				t.Fatalf("endpoint = %q, want %q", got, test.endpoint)
			}
		})
	}
}

func TestKind(t *testing.T) {
	if New().Kind() != ai.KindOpenAI {
		t.Fatal("New() must be the openai back-end")
	}
	if Compatible().Kind() != ai.KindCompatible {
		t.Fatal("Compatible() must be the compatible back-end")
	}
}

func TestCapabilities(t *testing.T) {
	cases := map[string]struct {
		endpoint string
		local    bool
	}{
		"openai itself":  {endpoint: DefaultEndpoint, local: false},
		"localhost":      {endpoint: "http://localhost:8080/v1", local: true},
		"a loopback ip":  {endpoint: "http://127.0.0.1:11434/v1", local: true},
		"the same in v6": {endpoint: "http://[::1]:8080/v1", local: true},
		"someone else":   {endpoint: "https://llm.internal/v1", local: false},
	}
	for name, test := range cases {
		t.Run(name, func(t *testing.T) {
			built, err := Compatible().Open(ai.Instance{Name: "work", Endpoint: test.endpoint, Context: 4096}, ai.Deps{HTTP: &recorded{}})
			if err != nil {
				t.Fatalf("Open() error = %v", err)
			}
			caps := built.Capabilities()
			if caps.Local != test.local {
				t.Fatalf("Local = %v, want %v for %s", caps.Local, test.local, test.endpoint)
			}
			if !caps.Tools || !caps.Streaming || caps.Context != 4096 {
				t.Fatalf("Capabilities() = %+v", caps)
			}
		})
	}
}

func TestChatDefaultsTheModelAndTheLimit(t *testing.T) {
	doer := &recorded{body: cassette(t, "text.sse")}
	client := client4Test(t, doer, ai.Instance{Model: "gpt-5"})

	if _, err := client.Chat(context.Background(), ai.Request{}); err != nil {
		t.Fatalf("Chat() error = %v", err)
	}
	var body map[string]any
	json.Unmarshal([]byte(doer.bodies[0]), &body)
	if body["model"] != "gpt-5" {
		t.Fatalf("model = %v, want the instance's own", body["model"])
	}
	if body["max_completion_tokens"] != float64(defaultMaxTokens) {
		t.Fatalf("max_completion_tokens = %v, want the default", body["max_completion_tokens"])
	}
	if _, ok := body["temperature"]; ok {
		t.Fatal("a temperature nobody chose must not be sent")
	}
}

func TestChatReportsARequestThatFailed(t *testing.T) {
	doer := &recorded{err: errors.New("connection refused")}
	client := client4Test(t, doer, ai.Instance{Model: "gpt-5"})

	response, err := client.Chat(context.Background(), ai.Request{})
	if response != nil {
		t.Fatal("a failed request must not return a stream")
	}
	if reason, _ := ai.ReasonOf(err); reason != ai.ReasonUnavailable {
		t.Fatalf("reason = %q, want unavailable", reason)
	}
}

func TestStreamCloses(t *testing.T) {
	doer := &recorded{body: cassette(t, "text.sse")}
	client := client4Test(t, doer, ai.Instance{Model: "gpt-5"})

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
}

func TestStreamEndsWhileStillThinking(t *testing.T) {
	doer := &recorded{body: `data: {"choices":[{"delta":{"reasoning":"still working"}}]}` + "\n\n"}
	client := client4Test(t, doer, ai.Instance{Model: "gpt-5"})

	stream, err := client.Chat(context.Background(), ai.Request{})
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}
	chunks := drain(t, stream)
	var closed bool
	for _, chunk := range chunks {
		if chunk.Kind == ai.ChunkReasoningEnd {
			closed = true
		}
	}
	if !closed {
		t.Fatal("a stream that ended mid-thought never closed the block")
	}
}

func TestStreamSkipsAnEmptyFrame(t *testing.T) {
	doer := &recorded{body: "data:\n\ndata: {\"choices\":[{\"delta\":{\"content\":\"hi\"}}]}\n\ndata: [DONE]\n\n"}
	client := client4Test(t, doer, ai.Instance{Model: "gpt-5"})

	stream, err := client.Chat(context.Background(), ai.Request{})
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}
	if got := spoken(drain(t, stream), ai.ChunkTextDelta); got != "hi" {
		t.Fatalf("text = %q", got)
	}
}

func TestStopReason(t *testing.T) {
	cases := map[string]ai.StopReason{
		"stop":           ai.StopEndTurn,
		"length":         ai.StopMaxTokens,
		"tool_calls":     ai.StopToolUse,
		"function_call":  ai.StopToolUse,
		"content_filter": ai.StopContentFilter,
		"something new":  ai.StopEndTurn,
	}
	for finish, want := range cases {
		t.Run(finish, func(t *testing.T) {
			if got := stopReason(finish); got != want {
				t.Fatalf("stopReason(%q) = %q, want %q", finish, got, want)
			}
		})
	}
}

func TestArguments(t *testing.T) {
	cases := map[string]struct {
		written string
		want    int
	}{
		"nothing written": {written: "", want: 0},
		"only spaces":     {written: "   ", want: 0},
		"unreadable":      {written: "{oops", want: 0},
		"two arguments":   {written: `{"a": 1, "b": 2}`, want: 2},
	}
	for name, test := range cases {
		t.Run(name, func(t *testing.T) {
			if got := arguments(test.written); len(got) != test.want {
				t.Fatalf("arguments(%q) = %v, want %d of them", test.written, got, test.want)
			}
		})
	}
}

func TestWrittenSkipsAToolTurnWithNothingInIt(t *testing.T) {
	if got := written(ai.Message{Role: ai.RoleTool}); got != nil {
		t.Fatalf("written() = %v, want nothing", got)
	}
}

func TestCallsSkipWhatCannotBeWrittenDown(t *testing.T) {
	got := calls([]ai.ToolCall{
		{ID: "call_1", Name: "broken", Arguments: map[string]any{"channel": make(chan int)}},
		{ID: "call_2", Name: "list_tables", Arguments: map[string]any{}},
	})
	if len(got) != 1 {
		t.Fatalf("calls() = %v, want the one that could be written kept", got)
	}
	if got[0]["id"] != "call_2" {
		t.Fatalf("calls() kept %v, want call_2", got[0]["id"])
	}
}

func TestAnEndpointThatIsNotAnAddress(t *testing.T) {
	built, err := Compatible().Open(ai.Instance{Name: "work", Endpoint: "://nowhere"}, ai.Deps{HTTP: &recorded{}})
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if _, err := built.Chat(context.Background(), ai.Request{}); err == nil {
		t.Fatal("Chat() must refuse an endpoint it cannot build a request from")
	} else if reason, _ := ai.ReasonOf(err); reason != ai.ReasonRequest {
		t.Fatalf("reason = %q, want request", reason)
	}
	if _, err := built.(ai.Lister).Models(context.Background()); err == nil {
		t.Fatal("Models() must refuse the same")
	}
}

type cutOff struct {
	data string
	sent bool
}

func (c *cutOff) Read(p []byte) (int, error) {
	if c.sent {
		return 0, errors.New("connection reset by peer")
	}
	c.sent = true
	return copy(p, c.data), nil
}

func (c *cutOff) Close() error { return nil }

func TestStreamReportsAConnectionThatDropped(t *testing.T) {
	reading := newStream("work", &cutOff{data: `data: {"choices":[{"delta":{"content":"half"}}]}` + "\n\n"})

	if _, err := reading.Next(); err != nil {
		t.Fatalf("Next() error = %v, want the first chunk", err)
	}
	for {
		_, err := reading.Next()
		if err == nil {
			continue
		}
		if reason, _ := ai.ReasonOf(err); reason != ai.ReasonProvider {
			t.Fatalf("reason = %q, want provider", reason)
		}
		return
	}
}
