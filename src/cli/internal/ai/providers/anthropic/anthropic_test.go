package anthropic

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
	status := r.status
	if status == 0 {
		status = http.StatusOK
	}
	return &http.Response{
		StatusCode: status,
		Body:       io.NopCloser(strings.NewReader(r.body)),
		Header:     http.Header{},
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
		instance.Name = "claude"
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
	client := client4Test(t, doer, ai.Instance{Model: "claude-sonnet-5", Key: []byte("sk-ant")})

	stream, err := client.Chat(context.Background(), ai.Request{
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
		t.Fatalf("last chunk = %+v", last)
	}
	if last.Usage.Input != 125 {
		t.Fatalf("input = %d, want the cache counted into the total", last.Usage.Input)
	}
	if last.Usage.NonCachedInput != 100 || last.Usage.CacheRead != 20 || last.Usage.CacheWrite != 5 {
		t.Fatalf("usage = %+v, want the parts kept apart", last.Usage)
	}
	if !last.Usage.Balanced() {
		t.Fatalf("usage = %+v, want the parts to add up", last.Usage)
	}
	if last.Usage.Total != 143 {
		t.Fatalf("total = %d, want input and output together", last.Usage.Total)
	}
}

func TestChatWritesTheRequest(t *testing.T) {
	doer := &recorded{body: cassette(t, "text.sse")}
	client := client4Test(t, doer, ai.Instance{Model: "claude-sonnet-5", Key: []byte("sk-ant")})

	_, err := client.Chat(context.Background(), ai.Request{
		System: "you read databases",
		Messages: []ai.Message{
			{Role: ai.RoleUser, Content: "which?"},
			{Role: ai.RoleAssistant, Content: "checking", Calls: []ai.ToolCall{{ID: "toolu_1", Name: "list_tables", Arguments: map[string]any{"schema": "main"}}}},
			{Role: ai.RoleTool, Result: &ai.ToolResult{ID: "toolu_1", Name: "list_tables", Content: "orders"}},
			{Role: ai.RoleTool, Result: &ai.ToolResult{ID: "toolu_2", Name: "list_indexes", Content: "none", Failed: true}},
		},
		Tools:    []ai.Tool{{Name: "list_tables", Description: "every table"}},
		Thinking: true,
	})
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}

	var body map[string]any
	if err := json.Unmarshal([]byte(doer.bodies[0]), &body); err != nil {
		t.Fatalf("the request was not json: %v", err)
	}
	if body["system"] != "you read databases" {
		t.Fatalf("system = %v, want it sent beside the messages rather than inside them", body["system"])
	}
	if body["max_tokens"] != float64(defaultMaxTokens) {
		t.Fatalf("max_tokens = %v, want the default", body["max_tokens"])
	}
	messages := body["messages"].([]any)
	if len(messages) != 3 {
		t.Fatalf("sent %d turns, want the two tool results folded into one", len(messages))
	}
	third := messages[2].(map[string]any)
	if third["role"] != "user" {
		t.Fatalf("the tool results were sent as %v, want them from the user", third["role"])
	}
	results := third["content"].([]any)
	if len(results) != 2 {
		t.Fatalf("the folded turn holds %d results, want 2", len(results))
	}
	if results[1].(map[string]any)["is_error"] != true {
		t.Fatal("a tool that refused was not marked as one")
	}
	second := messages[1].(map[string]any)["content"].([]any)
	if len(second) != 2 || second[1].(map[string]any)["type"] != "tool_use" {
		t.Fatalf("the assistant turn = %v, want its text and its call", second)
	}
	thinking := body["thinking"].(map[string]any)
	if thinking["type"] != "enabled" || thinking["budget_tokens"].(float64) >= body["max_tokens"].(float64) {
		t.Fatalf("thinking = %v, want a budget inside the answer's allowance", thinking)
	}
	request := doer.requests[0]
	if request.Header.Get("X-Api-Key") != "sk-ant" {
		t.Fatalf("x-api-key = %q", request.Header.Get("X-Api-Key"))
	}
	if request.Header.Get("Anthropic-Version") != Version {
		t.Fatalf("anthropic-version = %q, want %q", request.Header.Get("Anthropic-Version"), Version)
	}
	if got := request.URL.String(); got != DefaultEndpoint+"/messages" {
		t.Fatalf("url = %q", got)
	}
}

func TestChatGathersAToolCall(t *testing.T) {
	doer := &recorded{body: cassette(t, "tools.sse")}
	client := client4Test(t, doer, ai.Instance{Model: "claude-sonnet-5"})

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
	if call == nil || call.ID != "toolu_abc" || call.Name != "describe_table" {
		t.Fatalf("call = %+v", call)
	}
	if call.Arguments["schema"] != "main" || call.Arguments["table"] != "orders" {
		t.Fatalf("arguments = %v", call.Arguments)
	}
	if chunks[len(chunks)-1].Stop != ai.StopToolUse {
		t.Fatalf("stop = %q, want tool_use", chunks[len(chunks)-1].Stop)
	}
}

func TestChatSeparatesThinking(t *testing.T) {
	doer := &recorded{body: cassette(t, "thinking.sse")}
	client := client4Test(t, doer, ai.Instance{Model: "claude-sonnet-5"})

	stream, err := client.Chat(context.Background(), ai.Request{Thinking: true})
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}
	chunks := drain(t, stream)

	if got := spoken(chunks, ai.ChunkReasoningDelta); got != "the largest is what they mean" {
		t.Fatalf("reasoning = %q", got)
	}
	if got := spoken(chunks, ai.ChunkTextDelta); got != "orders" {
		t.Fatalf("text = %q", got)
	}
	if chunks[len(chunks)-1].Stop != ai.StopMaxTokens {
		t.Fatalf("stop = %q, want max_tokens", chunks[len(chunks)-1].Stop)
	}
}

func TestChatReportsAnErrorInTheStream(t *testing.T) {
	doer := &recorded{body: cassette(t, "error.sse")}
	client := client4Test(t, doer, ai.Instance{Model: "claude-sonnet-5"})

	stream, err := client.Chat(context.Background(), ai.Request{})
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}
	_, err = stream.Next()
	if reason, _ := ai.ReasonOf(err); reason != ai.ReasonContextOverflow {
		t.Fatalf("reason = %q, want the overflow read out of the message", reason)
	}
}

func TestProbe(t *testing.T) {
	doer := &recorded{body: cassette(t, "text.sse")}
	client := client4Test(t, doer, ai.Instance{Model: "claude-sonnet-5", Key: []byte("sk-ant")})

	if err := client.(ai.Prober).Probe(context.Background()); err != nil {
		t.Fatalf("Probe() error = %v", err)
	}
	var body map[string]any
	json.Unmarshal([]byte(doer.bodies[0]), &body)
	if body["max_tokens"] != float64(1) {
		t.Fatalf("max_tokens = %v, want a probe to cost as little as it can", body["max_tokens"])
	}
}

func TestProbeReportsARefusal(t *testing.T) {
	doer := &recorded{status: http.StatusUnauthorized, body: `{"error":{"message":"invalid x-api-key"}}`}
	client := client4Test(t, doer, ai.Instance{Model: "claude-sonnet-5"})

	err := client.(ai.Prober).Probe(context.Background())
	if reason, _ := ai.ReasonOf(err); reason != ai.ReasonAuth {
		t.Fatalf("reason = %q, want auth", reason)
	}
}

func TestOpen(t *testing.T) {
	if _, err := New().Open(ai.Instance{Name: "claude"}, ai.Deps{}); err == nil {
		t.Fatal("Open() must refuse an instance with no http client")
	}
	built, err := New().Open(ai.Instance{Name: "claude", Endpoint: "https://proxy.internal/v1/"}, ai.Deps{HTTP: &recorded{}})
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if got := built.(*client).endpoint; got != "https://proxy.internal/v1" {
		t.Fatalf("endpoint = %q, want the trailing slash gone", got)
	}
	if New().Kind() != ai.KindAnthropic {
		t.Fatal("Kind() must name the anthropic back-end")
	}
	caps := built.Capabilities()
	if !caps.Tools || !caps.Streaming || !caps.Reasoning || caps.Local {
		t.Fatalf("Capabilities() = %+v, want a remote model that can think and use tools", caps)
	}
}

func TestBudget(t *testing.T) {
	cases := map[string]struct {
		maxTokens int
		want      int
	}{
		"half of a large allowance": {maxTokens: 8000, want: 4000},
		"a floor for a small one":   {maxTokens: 100, want: minThinking},
		"exactly at the floor":      {maxTokens: 2048, want: minThinking},
	}
	for name, test := range cases {
		t.Run(name, func(t *testing.T) {
			if got := budget(test.maxTokens); got != test.want {
				t.Fatalf("budget(%d) = %d, want %d", test.maxTokens, got, test.want)
			}
		})
	}
}

func TestStopReason(t *testing.T) {
	cases := map[string]ai.StopReason{
		"end_turn":      ai.StopEndTurn,
		"stop_sequence": ai.StopEndTurn,
		"max_tokens":    ai.StopMaxTokens,
		"tool_use":      ai.StopToolUse,
		"refusal":       ai.StopContentFilter,
	}
	for from, want := range cases {
		t.Run(from, func(t *testing.T) {
			if got := stopReason(from); got != want {
				t.Fatalf("stopReason(%q) = %q, want %q", from, got, want)
			}
		})
	}
}

func TestMessagesSkipWhatHasNothingInIt(t *testing.T) {
	got := messages([]ai.Message{
		{Role: ai.RoleUser, Content: "   "},
		{Role: ai.RoleTool},
		{Role: ai.RoleAssistant},
		{Role: ai.RoleUser, Content: "which?"},
	})
	if len(got) != 1 {
		t.Fatalf("messages() = %v, want only the turn that said something", got)
	}
}

func TestStreamIgnoresWhatItDoesNotKnow(t *testing.T) {
	doer := &recorded{body: "event: ping\ndata: {\"type\":\"ping\"}\n\n" +
		"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":9,\"delta\":{\"type\":\"text_delta\",\"text\":\"lost\"}}\n\n" +
		"event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":9}\n\n" +
		"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"}
	client := client4Test(t, doer, ai.Instance{Model: "claude-sonnet-5"})

	stream, err := client.Chat(context.Background(), ai.Request{})
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}
	chunks := drain(t, stream)
	if got := spoken(chunks, ai.ChunkTextDelta); got != "" {
		t.Fatalf("text = %q, want a delta for a block that never opened ignored", got)
	}
	if len(chunks) != 1 || chunks[0].Kind != ai.ChunkDone {
		t.Fatalf("chunks = %v, want only the end", chunks)
	}
}

func TestStreamReportsAFrameItCannotRead(t *testing.T) {
	doer := &recorded{body: "event: message_delta\ndata: {not json}\n\n"}
	client := client4Test(t, doer, ai.Instance{Model: "claude-sonnet-5"})

	stream, err := client.Chat(context.Background(), ai.Request{})
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}
	if _, err := stream.Next(); err == nil {
		t.Fatal("Next() must refuse a frame it cannot read")
	}
}

func TestStreamCloses(t *testing.T) {
	doer := &recorded{body: cassette(t, "text.sse")}
	client := client4Test(t, doer, ai.Instance{Model: "claude-sonnet-5"})

	stream, err := client.Chat(context.Background(), ai.Request{})
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}
	if err := stream.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
}

func TestArguments(t *testing.T) {
	cases := map[string]int{"": 0, "   ": 0, "{oops": 0, `{"a":1}`: 1}
	for written, want := range cases {
		t.Run(written, func(t *testing.T) {
			if got := arguments(written); len(got) != want {
				t.Fatalf("arguments(%q) = %v, want %d", written, got, want)
			}
		})
	}
}

func TestChatSendsWhatWasChosen(t *testing.T) {
	doer := &recorded{body: cassette(t, "text.sse")}
	client := client4Test(t, doer, ai.Instance{Model: "claude-sonnet-5"})

	_, err := client.Chat(context.Background(), ai.Request{
		Model:       "claude-opus-5",
		MaxTokens:   64,
		Temperature: 0.2,
		TopP:        0.8,
		Stop:        []string{"###"},
	})
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}
	var body map[string]any
	json.Unmarshal([]byte(doer.bodies[0]), &body)
	if body["model"] != "claude-opus-5" {
		t.Fatalf("model = %v, want the one the request named", body["model"])
	}
	if body["max_tokens"] != float64(64) || body["temperature"] != 0.2 || body["top_p"] != 0.8 {
		t.Fatalf("body = %v, want what was chosen", body)
	}
	if len(body["stop_sequences"].([]any)) != 1 {
		t.Fatalf("stop_sequences = %v", body["stop_sequences"])
	}
	if _, ok := body["thinking"]; ok {
		t.Fatal("thinking nobody asked for must not be sent")
	}
}

func TestChatRefusesAnEndpointItCannotUse(t *testing.T) {
	built, err := New().Open(ai.Instance{Name: "claude", Endpoint: "://nowhere"}, ai.Deps{HTTP: &recorded{}})
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	stream, err := built.Chat(context.Background(), ai.Request{})
	if stream != nil {
		t.Fatal("a request that could not be built must not return a stream")
	}
	if reason, _ := ai.ReasonOf(err); reason != ai.ReasonRequest {
		t.Fatalf("reason = %q, want request", reason)
	}
}

func TestStreamHandlesTheOddFrames(t *testing.T) {
	body := strings.Join([]string{
		`event: content_block_start`,
		`data: {"type":"content_block_start","index":0}`,
		``,
		`event: content_block_start`,
		`data: {"type":"content_block_start","index":1,"content_block":{"type":"redacted_thinking"}}`,
		``,
		`event: content_block_delta`,
		`data: {"type":"content_block_delta","index":1,"delta":{"type":"signature_delta","signature":"abc"}}`,
		``,
		`event: content_block_stop`,
		`data: {"type":"content_block_stop","index":1}`,
		``,
		`event: message_delta`,
		`data: {"type":"message_delta","delta":{}}`,
		``,
		`event: error`,
		`data: {"type":"error"}`,
		``,
		``,
	}, "\n")
	client := client4Test(t, &recorded{body: body}, ai.Instance{Model: "claude-sonnet-5"})

	stream, err := client.Chat(context.Background(), ai.Request{})
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}
	var kinds []ai.ChunkKind
	for {
		chunk, err := stream.Next()
		if err != nil {
			if failure, _ := ai.ReasonOf(err); failure == "" {
				t.Fatalf("Next() error = %v, want a classified failure", err)
			}
			break
		}
		kinds = append(kinds, chunk.Kind)
	}
	want := []ai.ChunkKind{ai.ChunkReasoningStart, ai.ChunkReasoningEnd}
	if len(kinds) != len(want) {
		t.Fatalf("kinds = %v, want %v", kinds, want)
	}
	for i := range want {
		if kinds[i] != want[i] {
			t.Fatalf("chunk %d = %q, want %q", i, kinds[i], want[i])
		}
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
	reading := newStream("claude", &cutOff{data: "event: message_start\ndata: {\"type\":\"message_start\"}\n\n"})
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

func TestCountIgnoresWhatIsNotThere(t *testing.T) {
	reading := newStream("claude", io.NopCloser(strings.NewReader("")))
	reading.count(nil)
	reading.count(&tally{})
	if got := reading.total(); got.Input != 0 || got.Output != 0 || !got.Balanced() {
		t.Fatalf("total() = %+v, want nothing counted", got)
	}
}

func TestStreamStaysFinished(t *testing.T) {
	client := client4Test(t, &recorded{body: cassette(t, "text.sse")}, ai.Instance{Model: "claude-sonnet-5"})
	stream, err := client.Chat(context.Background(), ai.Request{})
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}
	drain(t, stream)
	if _, err := stream.Next(); !errors.Is(err, io.EOF) {
		t.Fatalf("Next() after the end = %v, want io.EOF again", err)
	}
}

func TestStreamSkipsAnEmptyFrame(t *testing.T) {
	body := "event: ping\ndata:\n\nevent: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"
	client := client4Test(t, &recorded{body: body}, ai.Instance{Model: "claude-sonnet-5"})

	stream, err := client.Chat(context.Background(), ai.Request{})
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}
	chunks := drain(t, stream)
	if len(chunks) != 1 || chunks[0].Kind != ai.ChunkDone {
		t.Fatalf("chunks = %v, want only the end", chunks)
	}
}
