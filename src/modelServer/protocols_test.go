package modelServer

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestProtocolMatrix(t *testing.T) {
	for _, protocol := range []string{"openai-chat", "openai-responses", "anthropic", "gemini", "ollama"} {
		for _, stream := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/stream=%v", protocol, stream), func(t *testing.T) {
				upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					var body map[string]any
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
						t.Fatal(err)
					}
					expected := map[string]string{"openai-chat": "/chat/completions", "openai-responses": "/responses", "anthropic": "/messages", "ollama": "/api/chat", "gemini": "/models/gpt-4o-mini:generateContent"}[protocol]
					if protocol == "gemini" && stream {
						expected = "/models/gpt-4o-mini:streamGenerateContent"
					}
					if r.URL.Path != expected {
						t.Errorf("path %s want %s", r.URL.Path, expected)
					}
					switch protocol {
					case "anthropic":
						if r.Header.Get("x-api-key") != "test" || r.Header.Get("anthropic-version") == "" || body["system"] == nil || body["messages"] == nil {
							t.Error("Claude contract")
						}
					case "gemini":
						if r.Header.Get("x-goog-api-key") != "test" || r.URL.Query().Get("key") != "" || body["systemInstruction"] == nil || body["contents"] == nil {
							t.Error("Gemini contract")
						}
					default:
						if r.Header.Get("Authorization") != "Bearer test" {
							t.Error("authorization")
						}
					}
					if protocol == "openai-responses" && (body["input"] == nil || body["store"] != false || body["max_output_tokens"] == nil) {
						t.Error("Responses contract")
					}
					if protocol == "openai-chat" && body["max_completion_tokens"] == nil {
						t.Error("token parameter")
					}
					raw := resultJSON()
					var envelope any
					switch protocol {
					case "openai-chat":
						envelope = map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": raw}, "finish_reason": "stop"}}}
					case "openai-responses":
						envelope = map[string]any{"status": "completed", "output": []any{map[string]any{"type": "message", "content": []any{map[string]string{"type": "output_text", "text": raw}}}}}
					case "anthropic":
						envelope = map[string]any{"content": []any{map[string]string{"type": "text", "text": raw}}, "stop_reason": "end_turn"}
					case "gemini":
						envelope = map[string]any{"candidates": []any{map[string]any{"content": map[string]any{"parts": []any{map[string]any{"text": "secret reasoning", "thought": true}, map[string]string{"text": raw}}}, "finishReason": "STOP"}}}
					case "ollama":
						envelope = map[string]any{"message": map[string]string{"content": raw, "thinking": "secret reasoning"}, "done": true, "done_reason": "stop"}
					}
					if !stream {
						w.Header().Set("Content-Type", "application/json")
						json.NewEncoder(w).Encode(envelope)
						return
					}
					if protocol == "ollama" {
						w.Header().Set("Content-Type", "application/x-ndjson")
						json.NewEncoder(w).Encode(envelope)
						return
					}
					w.Header().Set("Content-Type", "text/event-stream")
					send := func(v any) { data, _ := json.Marshal(v); fmt.Fprintf(w, "data: %s\n\n", data) }
					switch protocol {
					case "openai-chat":
						send(map[string]any{"choices": []any{map[string]any{"delta": map[string]string{"content": raw}, "finish_reason": "stop"}}})
					case "openai-responses":
						send(map[string]any{"type": "response.output_text.delta", "delta": raw})
						send(map[string]any{"type": "response.completed", "response": envelope})
					case "anthropic":
						send(map[string]any{"type": "content_block_delta", "delta": map[string]string{"type": "text_delta", "text": raw}})
						send(map[string]any{"type": "message_delta", "delta": map[string]string{"stop_reason": "end_turn"}})
						send(map[string]string{"type": "message_stop"})
					case "gemini":
						send(envelope)
					}
				}))
				defer upstream.Close()
				cfg := testConfig(upstream.URL)
				cfg.Namespace[0].Chatbot.Protocol = protocol
				cfg.Namespace[0].Chatbot.IsStream = stream
				cfg.Namespace[0].Chatbot.TokenParameter = "max_completion_tokens"
				p, err := NewProvider(cfg)
				if err != nil {
					t.Fatal(err)
				}
				result, err := p.Generate(context.Background(), Request{Question: "test"}, nil)
				if err != nil {
					t.Fatal(err)
				}
				if result.Answer != sampleResult().Answer {
					t.Fatal(result.Answer)
				}
			})
		}
	}
}
func TestProtocolRejectsIncompleteResponses(t *testing.T) {
	cases := []struct{ protocol, body string }{
		{"openai-responses", `data: {"type":"response.output_text.delta","delta":"partial"}`},
		{"openai-responses", `data: {"type":"response.incomplete"}`},
		{"anthropic", `data: {"type":"message_stop"}`},
		{"anthropic", `data: {"type":"message_delta","delta":{"stop_reason":"max_tokens"}}`},
		{"gemini", `data: {"candidates":[{"content":{"parts":[{"text":"partial"}]}}]}`},
		{"gemini", `data: {"candidates":[{"finishReason":"MAX_TOKENS"}]}`},
		{"gemini", `data: {"error":{"message":"do not echo secret"}}`},
	}
	for _, c := range cases {
		t.Run(c.protocol+c.body, func(t *testing.T) {
			if _, err := readProtocolSSE(strings.NewReader(c.body), c.protocol); err == nil {
				t.Fatal("accepted incomplete stream")
			}
		})
	}
	if _, err := readOllama(strings.NewReader(`{"message":{"content":"partial"},"done":false}`)); err == nil {
		t.Fatal("accepted partial Ollama")
	}
}
func TestManagedTransportAndRedirects(t *testing.T) {
	called := false
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true; respond(w, resultJSON()) }))
	defer target.Close()
	cfg := testConfig(target.URL)
	p, _ := NewManagedProvider(cfg, false)
	if _, err := p.Generate(context.Background(), Request{Question: "test"}, nil); err == nil || called {
		t.Fatal("remote install reached local service")
	}
	p, _ = NewManagedProvider(cfg, true)
	if _, err := p.Generate(context.Background(), Request{Question: "test"}, nil); err != nil || !called {
		t.Fatal(err)
	}
	called = false
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	}))
	defer redirect.Close()
	cfg.Namespace[0].Chatbot.BaseURL = redirect.URL
	p, _ = NewProvider(cfg)
	if _, err := p.Generate(context.Background(), Request{Question: "test"}, nil); err == nil || called {
		t.Fatal("followed credential redirect")
	}
	for _, raw := range []string{"https://user:secret@example.com/v1", "https://example.com/v1?key=secret", "http://169.254.169.254/", "ftp://example.com"} {
		if _, err := NormalizeEndpoint(raw, "openai-chat", false); err == nil {
			t.Fatal(raw)
		}
	}
}
