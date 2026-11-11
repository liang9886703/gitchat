package modelServer

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"gitchat/src/config"
)

func sampleResult() Result {
	return Result{Title: "A focused answer", Answer: "Start with one constraint.", Suggestions: []Suggestion{{Title: "Cost", Question: "What is the budget?", Score: .7}, {Title: "Goal", Question: "What is the objective?", Score: .95}}, Memory: Memory{Goal: "Make a choice"}}
}
func testConfig(url string) *config.Config {
	cfg := config.Defaults()
	cfg.Mode = "openai"
	cfg.Namespace[0].Chatbot.BaseURL = url
	cfg.Namespace[0].Chatbot.APIKey = "test"
	cfg.Namespace[0].Chatbot.IsStream = false
	cfg.Routing.Retries = 0
	return cfg
}
func respond(w http.ResponseWriter, content string) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": Message{Role: "assistant", Content: content}, "finish_reason": "stop"}}})
}
func resultJSON() string { value, _ := json.Marshal(sampleResult()); return string(value) }

func TestProviderRoutingRedactionAndSSE(t *testing.T) {
	var gotModel, gotBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Model    string
			Messages []Message
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		gotModel = body.Model
		for _, m := range body.Messages {
			gotBody += m.Content
		}
		if r.Header.Get("Authorization") != "Bearer test" {
			t.Error("missing authorization")
		}
		w.Header().Set("Content-Type", "text/event-stream")
		content := resultJSON()
		for _, part := range []string{content[:20], content[20:]} {
			event, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"delta": Message{Content: part}}}})
			fmt.Fprintf(w, ": keep-alive\ndata: %s\n\n", event)
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer server.Close()
	cfg := testConfig(server.URL)
	cfg.Routing.Topics = []config.TopicRoute{{Keywords: []string{"code"}, Model: "specialist"}}
	model, err := NewProvider(cfg)
	if err != nil {
		t.Fatal(err)
	}
	var streamed strings.Builder
	result, err := model.Generate(context.Background(), Request{Question: "code email me@example.com", SystemPrompt: "APP_PROMPT", Messages: []Message{{Role: "user", Content: "My phone is 13812345678"}}}, func(delta string) error { streamed.WriteString(delta); return nil })
	if err != nil {
		t.Fatal(err)
	}
	if gotModel != "specialist" || strings.Contains(gotBody, "me@example.com") || strings.Contains(gotBody, "13812345678") || !strings.Contains(gotBody, "APP_PROMPT") {
		t.Fatalf("incorrect route/privacy/prompt: %s %s", gotModel, gotBody)
	}
	if streamed.String() != result.Answer || result.Suggestions[0].Title != "Goal" {
		t.Fatalf("incorrect result: %+v", result)
	}
}
func TestProviderFallbackAndValidation(t *testing.T) {
	var models []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct{ Model string }
		_ = json.NewDecoder(r.Body).Decode(&body)
		models = append(models, body.Model)
		if body.Model == "strong" {
			respond(w, `{"answer":"bad schema"}`)
			return
		}
		respond(w, resultJSON())
	}))
	defer server.Close()
	cfg := testConfig(server.URL)
	cfg.Routing.StrongModel = "strong"
	cfg.Routing.FallbackModel = "fallback"
	cfg.Routing.Retries = 1
	model, _ := NewProvider(cfg)
	result, err := model.Generate(context.Background(), Request{Question: "Compare", Operation: "merge"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Model != "fallback" || strings.Join(models, ",") != "strong,strong,fallback" {
		t.Fatalf("unexpected fallback %v %+v", models, result)
	}
}
func TestCompressionOnModelSwitch(t *testing.T) {
	calls := 0
	var generation []Message
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct{ Messages []Message }
		_ = json.NewDecoder(r.Body).Decode(&body)
		calls++
		if calls == 1 {
			if !strings.Contains(body.Messages[0].Content, "Summarize") {
				t.Error("expected summary")
			}
			respond(w, "Earlier agreed constraint")
			return
		}
		generation = body.Messages
		respond(w, resultJSON())
	}))
	defer server.Close()
	model, _ := NewProvider(testConfig(server.URL))
	_, err := model.Generate(context.Background(), Request{Question: "continue", PreviousModel: "different", Messages: []Message{{Role: "user", Content: "original private context"}, {Role: "assistant", Content: "a response"}}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatalf("expected summary and answer, got %d", calls)
	}
	serialized, _ := json.Marshal(generation)
	if !strings.Contains(string(serialized), "Earlier agreed constraint") || strings.Contains(string(serialized), "original private context") {
		t.Fatalf("bad compact context: %s", serialized)
	}
}
func TestKnowledgeAndReadOnlyMCP(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "architecture.md"), []byte("Database scaling needs an explicit latency budget."), 0600); err != nil {
		t.Fatal(err)
	}
	var methods []string
	var gotPrompt string
	mcp := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var call struct {
			Method string
			Params struct {
				Name      string
				Arguments map[string]string
			}
		}
		_ = json.NewDecoder(r.Body).Decode(&call)
		methods = append(methods, call.Method)
		if call.Method != "initialize" && r.Header.Get("Mcp-Session-Id") != "test-session" {
			t.Error("lost MCP session")
		}
		w.Header().Set("Content-Type", "application/json")
		var value any
		switch call.Method {
		case "initialize":
			w.Header().Set("Mcp-Session-Id", "test-session")
			value = map[string]string{"protocolVersion": "2025-03-26"}
		case "notifications/initialized":
			w.WriteHeader(http.StatusAccepted)
			return
		case "tools/list":
			value = map[string]any{"tools": []any{map[string]any{"name": "search", "annotations": map[string]bool{"readOnlyHint": true}}, map[string]any{"name": "delete", "annotations": map[string]bool{"readOnlyHint": false}}}}
		case "tools/call":
			if call.Params.Name != "search" || call.Params.Arguments["query"] != "database scaling" {
				t.Error("unexpected tool call")
			}
			value = map[string]any{"content": []any{map[string]string{"type": "text", "text": "Search found reference 42"}}}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": 1, "result": value})
	}))
	defer mcp.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct{ Messages []Message }
		_ = json.NewDecoder(r.Body).Decode(&body)
		for _, m := range body.Messages {
			gotPrompt += m.Content
		}
		respond(w, resultJSON())
	}))
	defer server.Close()
	cfg := testConfig(server.URL)
	cfg.KnowledgeDir = dir
	cfg.MCPServers = []config.MCPServer{{URL: mcp.URL, Tools: []string{"search", "delete"}}}
	model, _ := NewProvider(cfg)
	_, err := model.Generate(context.Background(), Request{Question: "database scaling"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(methods, ",") != "initialize,notifications/initialized,tools/list,tools/call" {
		t.Fatalf("unexpected methods %v", methods)
	}
	if !strings.Contains(gotPrompt, "[source: architecture.md]") || !strings.Contains(gotPrompt, "Search found reference 42") {
		t.Fatalf("missing enrichment %s", gotPrompt)
	}
}
func TestCancellationAndBrokenStream(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		select {
		case <-r.Context().Done():
		case <-time.After(time.Second):
		}
	}))
	defer server.Close()
	model, _ := NewProvider(testConfig(server.URL))
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	_, err := model.Generate(ctx, Request{Question: "hello"}, nil)
	if err == nil {
		t.Fatal("expected cancellation")
	}
	_, err = readCompletionSSE(strings.NewReader("data: {\"choices\":[{\"delta\":{\"content\":\"incomplete\"}}]}\n\n"))
	if err == nil {
		t.Fatal("expected incomplete stream error")
	}
}
func TestConcurrentDemoAndLegacyCancellation(t *testing.T) {
	cfg := config.Defaults()
	cfg.Mode = "demo"
	model, _ := NewProvider(cfg)
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			result, err := model.Generate(context.Background(), Request{Question: "日本旅行"}, nil)
			if err != nil || result.Model != "demo" {
				t.Errorf("demo failed %v", err)
			}
		}()
	}
	wg.Wait()
	ctx, cancel := context.WithCancel(context.Background())
	ch := make(chan StreamMessage)
	stream := RecvStreamContext{Ctx: &ctx, Cancel: &cancel, Ch: &ch}
	stream.StopRecvStream()
	if ctx.Err() == nil {
		t.Fatal("stream was not cancelled")
	}
}
func TestProviderRejectsInvalidConfiguration(t *testing.T) {
	cfg := testConfig("file:///etc/passwd")
	if _, err := NewProvider(cfg); err == nil {
		t.Fatal("accepted invalid URL")
	}
	cfg = testConfig("http://localhost")
	cfg.Routing.Retries = -1
	if _, err := NewProvider(cfg); err == nil {
		t.Fatal("accepted negative retries")
	}
}

func TestPrivacyCheckCoversHistoryAndStopsGeneration(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		var body struct{ Messages []Message }
		_ = json.NewDecoder(r.Body).Decode(&body)
		serialized, _ := json.Marshal(body.Messages)
		if !strings.Contains(string(serialized), "history marker") || strings.Contains(string(serialized), "me@example.com") {
			t.Errorf("privacy check did not cover sanitized history: %s", serialized)
		}
		respond(w, `{"sensitive":true}`)
	}))
	defer server.Close()
	cfg := testConfig(server.URL)
	cfg.Privacy.ModelCheck = true
	model, _ := NewProvider(cfg)
	_, err := model.Generate(context.Background(), Request{Question: "continue", PreviousModel: "other", Messages: []Message{{Role: "user", Content: "history marker me@example.com"}}}, nil)
	if err == nil || calls != 1 {
		t.Fatalf("sensitive request proceeded: calls=%d err=%v", calls, err)
	}
}

func TestMCPRejectsUnsupportedProtocol(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": 1, "result": map[string]string{"protocolVersion": "2099-01-01"}})
	}))
	defer server.Close()
	cfg := testConfig("http://localhost")
	cfg.MCPServers = []config.MCPServer{{URL: server.URL, Tools: []string{"search"}}}
	model, _ := NewProvider(cfg)
	_, err := model.(*provider).retrieveTools(context.Background(), "question")
	if err == nil {
		t.Fatal("accepted unsupported MCP protocol")
	}
}
