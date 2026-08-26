package gemini

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
	return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(r.body)), Header: http.Header{}}, nil
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
		instance.Name = "gemini"
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
	client := client4Test(t, doer, ai.Instance{Model: "gemini-3-pro", Key: []byte("k-123")})

	stream, err := client.Chat(context.Background(), ai.Request{Messages: []ai.Message{{Role: ai.RoleUser, Content: "which?"}}})
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}
	chunks := drain(t, stream)

	if got := spoken(chunks, ai.ChunkTextDelta); got != "The orders table is the biggest." {
		t.Fatalf("text = %q", got)
	}
	last := chunks[len(chunks)-1]
	if last.Stop != ai.StopEndTurn || last.Usage == nil {
		t.Fatalf("last chunk = %+v", last)
	}
	if !last.Usage.Balanced() {
		t.Fatalf("usage = %+v, want the parts of the input to add up", last.Usage)
	}
	if last.Usage.VisibleOutput() != 12 {
		t.Fatalf("visible output = %d, want the thoughts taken off", last.Usage.VisibleOutput())
	}
}

func TestChatWritesTheRequest(t *testing.T) {
	doer := &recorded{body: cassette(t, "text.sse")}
	client := client4Test(t, doer, ai.Instance{Model: "gemini-3-pro", Key: []byte("k-123")})

	_, err := client.Chat(context.Background(), ai.Request{
		System: "you read databases",
		Messages: []ai.Message{
			{Role: ai.RoleUser, Content: "which?"},
			{Role: ai.RoleAssistant, Content: "checking", Calls: []ai.ToolCall{{Name: "list_tables", Arguments: map[string]any{"schema": "main"}}}},
			{Role: ai.RoleTool, Result: &ai.ToolResult{Name: "list_tables", Content: "orders"}},
			{Role: ai.RoleTool, Result: &ai.ToolResult{Name: "list_indexes", Content: "none"}},
		},
		Tools:       []ai.Tool{{Name: "list_tables"}},
		Temperature: 0.3,
		TopK:        40,
	})
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}

	var body map[string]any
	if err := json.Unmarshal([]byte(doer.bodies[0]), &body); err != nil {
		t.Fatalf("the request was not json: %v", err)
	}
	contents := body["contents"].([]any)
	if len(contents) != 3 {
		t.Fatalf("sent %d turns, want the two results folded into one", len(contents))
	}
	if contents[1].(map[string]any)["role"] != "model" {
		t.Fatalf("the assistant turn = %v, want it named model", contents[1])
	}
	if _, ok := body["systemInstruction"]; !ok {
		t.Fatal("the system instruction was not sent")
	}
	generation := body["generationConfig"].(map[string]any)
	if generation["temperature"] != 0.3 || generation["topK"] != float64(40) {
		t.Fatalf("generationConfig = %v", generation)
	}
	request := doer.requests[0]
	if request.URL.Query().Get("key") != "k-123" {
		t.Fatalf("key = %q, want it in the query", request.URL.Query().Get("key"))
	}
	if request.URL.Query().Get("alt") != "sse" {
		t.Fatal("the answer was not asked for as a stream")
	}
	if !strings.Contains(request.URL.Path, "models/gemini-3-pro:streamGenerateContent") {
		t.Fatalf("path = %q", request.URL.Path)
	}
}

func TestChatHandsOverAWholeCall(t *testing.T) {
	doer := &recorded{body: cassette(t, "tools.sse")}
	client := client4Test(t, doer, ai.Instance{Model: "gemini-3-pro"})

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
	if call == nil || call.Name != "describe_table" || call.ID != "call_1" {
		t.Fatalf("call = %+v", call)
	}
	if call.Arguments["table"] != "orders" {
		t.Fatalf("arguments = %v", call.Arguments)
	}
	if chunks[len(chunks)-1].Stop != ai.StopToolUse {
		t.Fatalf("stop = %q, want tool_use even though the protocol said stop", chunks[len(chunks)-1].Stop)
	}
}

func TestChatSeparatesThought(t *testing.T) {
	doer := &recorded{body: cassette(t, "thinking.sse")}
	client := client4Test(t, doer, ai.Instance{Model: "gemini-3-pro"})

	stream, err := client.Chat(context.Background(), ai.Request{})
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}
	chunks := drain(t, stream)

	if got := spoken(chunks, ai.ChunkReasoningDelta); got != "they mean the largest" {
		t.Fatalf("reasoning = %q", got)
	}
	if got := spoken(chunks, ai.ChunkTextDelta); got != "orders" {
		t.Fatalf("text = %q", got)
	}
	if chunks[len(chunks)-1].Stop != ai.StopMaxTokens {
		t.Fatalf("stop = %q", chunks[len(chunks)-1].Stop)
	}
}

func TestChatReportsAnErrorInTheStream(t *testing.T) {
	doer := &recorded{body: cassette(t, "error.sse")}
	client := client4Test(t, doer, ai.Instance{Model: "gemini-3-pro"})

	stream, err := client.Chat(context.Background(), ai.Request{})
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}
	_, err = stream.Next()
	if reason, _ := ai.ReasonOf(err); reason != ai.ReasonRateLimit {
		t.Fatalf("reason = %q, want the code read as a rate limit", reason)
	}
}

func TestModels(t *testing.T) {
	doer := &recorded{body: `{"models":[{"name":"models/gemini-3-pro","displayName":"Gemini 3 Pro","inputTokenLimit":1000000}]}`}
	client := client4Test(t, doer, ai.Instance{Model: "gemini-3-pro", Key: []byte("k-123")})

	listed, err := client.(ai.Lister).Models(context.Background())
	if err != nil {
		t.Fatalf("Models() error = %v", err)
	}
	if len(listed) != 1 || listed[0].ID != "gemini-3-pro" || listed[0].Context != 1000000 {
		t.Fatalf("Models() = %+v, want the prefix taken off the name", listed)
	}
	if err := client.(ai.Prober).Probe(context.Background()); err != nil {
		t.Fatalf("Probe() error = %v", err)
	}
}

func TestModelsReportsRubbish(t *testing.T) {
	client := client4Test(t, &recorded{body: "not json"}, ai.Instance{Model: "gemini-3-pro"})
	_, err := client.(ai.Lister).Models(context.Background())
	if reason, _ := ai.ReasonOf(err); reason != ai.ReasonDecode {
		t.Fatalf("reason = %q, want decode", reason)
	}
}

func TestOpen(t *testing.T) {
	if _, err := New().Open(ai.Instance{Name: "gemini"}, ai.Deps{}); err == nil {
		t.Fatal("Open() must refuse an instance with no http client")
	}
	built, err := New().Open(ai.Instance{Name: "gemini", Endpoint: "https://proxy.internal/v1beta/", Context: 8192}, ai.Deps{HTTP: &recorded{}})
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if got := built.(*client).endpoint; got != "https://proxy.internal/v1beta" {
		t.Fatalf("endpoint = %q", got)
	}
	if New().Kind() != ai.KindGemini {
		t.Fatal("Kind() must name the gemini back-end")
	}
	if caps := built.Capabilities(); !caps.Tools || caps.Context != 8192 {
		t.Fatalf("Capabilities() = %+v", caps)
	}
}

func TestAddressWithoutAKey(t *testing.T) {
	built, err := New().Open(ai.Instance{Name: "gemini"}, ai.Deps{HTTP: &recorded{}})
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if got := built.(*client).address("models", ""); got != DefaultEndpoint+"/models" {
		t.Fatalf("address() = %q, want no query at all", got)
	}
}

func TestChatRefusesAnEndpointItCannotUse(t *testing.T) {
	built, err := New().Open(ai.Instance{Name: "gemini", Endpoint: "://nowhere"}, ai.Deps{HTTP: &recorded{}})
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if _, err := built.Chat(context.Background(), ai.Request{}); err == nil {
		t.Fatal("Chat() must refuse an endpoint it cannot build a request from")
	}
	if _, err := built.(ai.Lister).Models(context.Background()); err == nil {
		t.Fatal("Models() must refuse the same")
	}
}

func TestContentsSkipWhatHasNothingInIt(t *testing.T) {
	got := contents([]ai.Message{
		{Role: ai.RoleUser, Content: "  "},
		{Role: ai.RoleTool},
		{Role: ai.RoleAssistant},
		{Role: ai.RoleUser, Content: "which?"},
	})
	if len(got) != 1 {
		t.Fatalf("contents() = %v, want only the turn that said something", got)
	}
}

func TestStopReason(t *testing.T) {
	cases := map[string]ai.StopReason{
		"STOP":               ai.StopEndTurn,
		"MAX_TOKENS":         ai.StopMaxTokens,
		"SAFETY":             ai.StopContentFilter,
		"RECITATION":         ai.StopContentFilter,
		"PROHIBITED_CONTENT": ai.StopContentFilter,
		"BLOCKLIST":          ai.StopContentFilter,
		"OTHER":              ai.StopEndTurn,
	}
	for from, want := range cases {
		t.Run(from, func(t *testing.T) {
			if got := stopReason(from); got != want {
				t.Fatalf("stopReason(%q) = %q, want %q", from, got, want)
			}
		})
	}
}

func TestStreamHandlesTheOddFrames(t *testing.T) {
	body := strings.Join([]string{
		`data:`,
		``,
		`data: {"candidates":[{"content":{"parts":[{"text":"","thought":true},{"functionCall":{"name":"a"}}]}}]}`,
		``,
		``,
	}, "\n")
	client := client4Test(t, &recorded{body: body}, ai.Instance{Model: "gemini-3-pro"})

	stream, err := client.Chat(context.Background(), ai.Request{})
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}
	chunks := drain(t, stream)
	var call *ai.ToolCall
	for _, chunk := range chunks {
		if chunk.Kind == ai.ChunkToolEnd {
			call = chunk.Tool
		}
	}
	if call == nil || call.Name != "a" || call.Arguments == nil {
		t.Fatalf("call = %+v, want a call with no arguments rather than none at all", call)
	}
}

func TestStreamReportsAFrameItCannotRead(t *testing.T) {
	client := client4Test(t, &recorded{body: "data: {not json}\n\n"}, ai.Instance{Model: "gemini-3-pro"})
	stream, err := client.Chat(context.Background(), ai.Request{})
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}
	if _, err := stream.Next(); err == nil {
		t.Fatal("Next() must refuse a frame it cannot read")
	}
}

func TestStreamCloses(t *testing.T) {
	client := client4Test(t, &recorded{body: cassette(t, "text.sse")}, ai.Instance{Model: "gemini-3-pro"})
	stream, err := client.Chat(context.Background(), ai.Request{})
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}
	drain(t, stream)
	if _, err := stream.Next(); !errors.Is(err, io.EOF) {
		t.Fatalf("Next() after the end = %v, want io.EOF", err)
	}
	if err := stream.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
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
	reading := newStream("gemini", &cutOff{data: `data: {"candidates":[{"content":{"parts":[{"text":"half"}]}}]}` + "\n\n"})
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

func TestChatSendsEveryLimitThatWasChosen(t *testing.T) {
	doer := &recorded{body: cassette(t, "text.sse")}
	client := client4Test(t, doer, ai.Instance{Model: "gemini-3-pro"})

	_, err := client.Chat(context.Background(), ai.Request{MaxTokens: 128, TopP: 0.7, Stop: []string{"###"}})
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}
	var body map[string]any
	json.Unmarshal([]byte(doer.bodies[0]), &body)
	generation := body["generationConfig"].(map[string]any)
	if generation["maxOutputTokens"] != float64(128) || generation["topP"] != 0.7 {
		t.Fatalf("generationConfig = %v", generation)
	}
	if len(generation["stopSequences"].([]any)) != 1 {
		t.Fatalf("stopSequences = %v", generation["stopSequences"])
	}
}

func TestStreamClosesAThoughtBeforeACall(t *testing.T) {
	body := `data: {"candidates":[{"content":{"parts":[{"text":"deciding","thought":true},{"functionCall":{"name":"a","args":{}}}]}}]}` + "\n\n"
	client := client4Test(t, &recorded{body: body}, ai.Instance{Model: "gemini-3-pro"})

	stream, err := client.Chat(context.Background(), ai.Request{})
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}
	var kinds []ai.ChunkKind
	for _, chunk := range drain(t, stream) {
		kinds = append(kinds, chunk.Kind)
	}
	want := []ai.ChunkKind{
		ai.ChunkReasoningStart, ai.ChunkReasoningDelta, ai.ChunkReasoningEnd,
		ai.ChunkToolStart, ai.ChunkToolEnd, ai.ChunkDone,
	}
	if len(kinds) != len(want) {
		t.Fatalf("kinds = %v, want %v", kinds, want)
	}
	for i := range want {
		if kinds[i] != want[i] {
			t.Fatalf("chunk %d = %q, want %q", i, kinds[i], want[i])
		}
	}
}

func TestStreamEndsWhileStillThinking(t *testing.T) {
	body := `data: {"candidates":[{"content":{"parts":[{"text":"still going","thought":true}]}}]}` + "\n\n"
	client := client4Test(t, &recorded{body: body}, ai.Instance{Model: "gemini-3-pro"})

	stream, err := client.Chat(context.Background(), ai.Request{})
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}
	var closed bool
	for _, chunk := range drain(t, stream) {
		if chunk.Kind == ai.ChunkReasoningEnd {
			closed = true
		}
	}
	if !closed {
		t.Fatal("a stream that ended mid-thought never closed the block")
	}
}
