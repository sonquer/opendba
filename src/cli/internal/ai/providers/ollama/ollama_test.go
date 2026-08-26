package ollama

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
		instance.Name = "ollama"
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
	doer := &recorded{body: cassette(t, "text.ndjson")}
	client := client4Test(t, doer, ai.Instance{Model: "qwen3.5:9b"})

	stream, err := client.Chat(context.Background(), ai.Request{Messages: []ai.Message{{Role: ai.RoleUser, Content: "which?"}}})
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
	if last.Usage == nil || last.Usage.Input != 120 || last.Usage.Output != 18 || !last.Usage.Balanced() {
		t.Fatalf("usage = %+v", last.Usage)
	}
}

func TestChatWritesTheRequest(t *testing.T) {
	doer := &recorded{body: cassette(t, "text.ndjson")}
	client := client4Test(t, doer, ai.Instance{Model: "qwen3.5:9b", Context: 8192})

	_, err := client.Chat(context.Background(), ai.Request{
		System: "you read databases",
		Messages: []ai.Message{
			{Role: ai.RoleUser, Content: "which?"},
			{Role: ai.RoleAssistant, Content: "checking", Calls: []ai.ToolCall{{Name: "list_tables", Arguments: map[string]any{"schema": "main"}}}},
			{Role: ai.RoleTool, Result: &ai.ToolResult{Name: "list_tables", Content: "orders"}},
			{Role: ai.RoleTool},
		},
		Tools:       []ai.Tool{{Name: "list_tables"}},
		Temperature: 0.4,
		TopP:        0.9,
		TopK:        40,
		MaxTokens:   256,
		Stop:        []string{"###"},
		Thinking:    true,
	})
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}

	var body map[string]any
	if err := json.Unmarshal([]byte(doer.bodies[0]), &body); err != nil {
		t.Fatalf("the request was not json: %v", err)
	}
	messages := body["messages"].([]any)
	if len(messages) != 4 {
		t.Fatalf("sent %d messages, want the empty tool turn left out", len(messages))
	}
	if messages[3].(map[string]any)["tool_name"] != "list_tables" {
		t.Fatalf("the tool result = %v, want it named", messages[3])
	}
	options := body["options"].(map[string]any)
	if options["num_ctx"] != float64(8192) || options["num_predict"] != float64(256) {
		t.Fatalf("options = %v", options)
	}
	if options["temperature"] != 0.4 || options["top_p"] != 0.9 || options["top_k"] != float64(40) {
		t.Fatalf("options = %v", options)
	}
	if body["think"] != true {
		t.Fatal("thinking was asked for and not sent")
	}
	if got := doer.requests[0].URL.String(); got != DefaultEndpoint+"/api/chat" {
		t.Fatalf("url = %q", got)
	}
}

func TestChatHandsOverAWholeCall(t *testing.T) {
	doer := &recorded{body: cassette(t, "tools.ndjson")}
	client := client4Test(t, doer, ai.Instance{Model: "qwen3.5:9b"})

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
	if call == nil || call.Name != "describe_table" || call.Arguments["table"] != "orders" {
		t.Fatalf("call = %+v", call)
	}
	if chunks[len(chunks)-1].Stop != ai.StopToolUse {
		t.Fatalf("stop = %q, want tool_use", chunks[len(chunks)-1].Stop)
	}
}

func TestChatSeparatesThinking(t *testing.T) {
	doer := &recorded{body: cassette(t, "thinking.ndjson")}
	client := client4Test(t, doer, ai.Instance{Model: "qwen3.5:9b"})

	stream, err := client.Chat(context.Background(), ai.Request{Thinking: true})
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

func TestChatReportsAnErrorLine(t *testing.T) {
	doer := &recorded{body: `{"error":"model 'qwen' not found, try pulling it first"}` + "\n"}
	client := client4Test(t, doer, ai.Instance{Model: "qwen"})

	stream, err := client.Chat(context.Background(), ai.Request{})
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}
	if _, err := stream.Next(); err == nil {
		t.Fatal("Next() must report what the daemon said")
	} else if !strings.Contains(err.Error(), "not found") {
		t.Fatalf("error = %v, want the daemon's own words", err)
	}
}

func TestChatReportsALineItCannotRead(t *testing.T) {
	client := client4Test(t, &recorded{body: "{not json}\n"}, ai.Instance{Model: "qwen3.5:9b"})
	stream, err := client.Chat(context.Background(), ai.Request{})
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}
	if _, err := stream.Next(); err == nil {
		t.Fatal("Next() must refuse a line it cannot read")
	}
}

func TestModelsAndProbe(t *testing.T) {
	doer := &recorded{body: `{"models":[{"name":"qwen3.5:9b","details":{"parameter_size":"9.0B","quantization_level":"Q4_K_M"}},{"name":"gemma4:e4b","details":{}}]}`}
	client := client4Test(t, doer, ai.Instance{Model: "qwen3.5:9b"})

	listed, err := client.(ai.Lister).Models(context.Background())
	if err != nil {
		t.Fatalf("Models() error = %v", err)
	}
	if len(listed) != 2 {
		t.Fatalf("Models() = %+v", listed)
	}
	if listed[0].Title != "qwen3.5:9b · 9.0B · Q4_K_M" {
		t.Fatalf("title = %q, want the size and the quantisation beside the name", listed[0].Title)
	}
	if listed[1].Title != "gemma4:e4b" {
		t.Fatalf("title = %q, want just the name when nothing else is known", listed[1].Title)
	}
	if err := client.(ai.Prober).Probe(context.Background()); err != nil {
		t.Fatalf("Probe() error = %v", err)
	}
	if got := doer.requests[0].URL.Path; got != "/api/tags" {
		t.Fatalf("path = %q", got)
	}
}

func TestProbe(t *testing.T) {
	cases := map[string]struct {
		model string
		body  string
		want  string
	}{
		"a model that was pulled":  {model: "qwen3.5:9b", body: `{"models":[{"name":"qwen3.5:9b"}]}`},
		"a model without its tag":  {model: "qwen3.5", body: `{"models":[{"name":"qwen3.5:9b"}]}`},
		"no model named at all":    {model: "", body: `{"models":[]}`},
		"a model that was not":     {model: "gemma4", body: `{"models":[{"name":"qwen3.5:9b"}]}`, want: "has not pulled"},
		"a daemon that is not run": {model: "qwen3.5:9b", body: "not json", want: "read the model list"},
	}
	for name, test := range cases {
		t.Run(name, func(t *testing.T) {
			client := client4Test(t, &recorded{body: test.body}, ai.Instance{Model: test.model})
			err := client.(ai.Prober).Probe(context.Background())
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

func TestOpen(t *testing.T) {
	if _, err := New().Open(ai.Instance{Name: "ollama"}, ai.Deps{}); err == nil {
		t.Fatal("Open() must refuse an instance with no http client")
	}
	if New().Kind() != ai.KindOllama {
		t.Fatal("Kind() must name the ollama back-end")
	}
	built, err := New().Open(ai.Instance{Name: "ollama", Endpoint: "http://box.internal:11434/"}, ai.Deps{HTTP: &recorded{}})
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if built.Capabilities().Local {
		t.Fatal("a daemon on another machine is not local")
	}
	here, err := New().Open(ai.Instance{Name: "ollama"}, ai.Deps{HTTP: &recorded{}})
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if !here.Capabilities().Local {
		t.Fatal("a daemon on this machine is local")
	}
}

func TestChatRefusesAnEndpointItCannotUse(t *testing.T) {
	built, err := New().Open(ai.Instance{Name: "ollama", Endpoint: "://nowhere"}, ai.Deps{HTTP: &recorded{}})
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

func TestStreamEndsWhileStillThinking(t *testing.T) {
	client := client4Test(t, &recorded{body: `{"message":{"thinking":"still going"},"done":false}` + "\n"}, ai.Instance{Model: "qwen3.5:9b"})
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

func TestStreamClosesAThoughtBeforeACall(t *testing.T) {
	body := `{"message":{"thinking":"deciding","tool_calls":[{"function":{"name":"a"}}]},"done":true}` + "\n"
	client := client4Test(t, &recorded{body: body}, ai.Instance{Model: "qwen3.5:9b"})

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

func TestStreamStaysFinished(t *testing.T) {
	client := client4Test(t, &recorded{body: cassette(t, "text.ndjson")}, ai.Instance{Model: "qwen3.5:9b"})
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
	reading := newStream("ollama", &cutOff{data: `{"message":{"content":"half"},"done":false}` + "\n"})
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
