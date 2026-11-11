package router

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"gitchat/src/cards"
	"gitchat/src/config"
	"gitchat/src/modelServer"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strings"
	"testing"
)

type localModel struct{}

func (localModel) Generate(ctx context.Context, req modelServer.Request, emit func(string) error) (modelServer.Result, error) {
	answer := "回答：" + req.Question
	if err := emit(answer); err != nil {
		return modelServer.Result{}, err
	}
	return modelServer.Result{Title: req.Question, Answer: answer, Model: "test", Suggestions: []modelServer.Suggestion{{Title: "继续", Question: "下一步呢", Score: 1}}, Memory: modelServer.Memory{Goal: req.Question}}, nil
}
func newHTTPTest(t *testing.T) (*httptest.Server, *http.Client) {
	t.Helper()
	server, err := cards.CreateCardServerWithProvider(&config.Config{DataDir: t.TempDir(), Mode: "demo"}, localModel{})
	if err != nil {
		t.Fatal(err)
	}
	h := httptest.NewServer(SetupRouter(&server))
	t.Cleanup(func() { h.Close(); _ = server.Close() })
	jar, _ := cookiejar.New(nil)
	return h, &http.Client{Jar: jar}
}
func apiRequest(t *testing.T, client *http.Client, method, url string, body any, want int, out any) {
	t.Helper()
	data, _ := json.Marshal(body)
	req, _ := http.NewRequest(method, url, bytes.NewReader(data))
	req.Header.Set("Content-Type", "application/json")
	res, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	if res.StatusCode != want {
		t.Fatalf("%s %s: status %d want %d: %s", method, url, res.StatusCode, want, raw)
	}
	if out != nil {
		if err := json.Unmarshal(raw, out); err != nil {
			t.Fatalf("decode %s: %v", raw, err)
		}
	}
}
func streamCard(t *testing.T, client *http.Client, url string, input cards.ChatInput) cards.Card {
	t.Helper()
	data, _ := json.Marshal(input)
	req, _ := http.NewRequest("POST", url, bytes.NewReader(data))
	req.Header.Set("Content-Type", "application/json")
	res, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		b, _ := io.ReadAll(res.Body)
		t.Fatalf("chat %d: %s", res.StatusCode, b)
	}
	if !strings.HasPrefix(res.Header.Get("Content-Type"), "text/event-stream") {
		t.Fatal("missing SSE content type")
	}
	scanner := bufio.NewScanner(res.Body)
	var result cards.Card
	completed := false
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		var e cards.Event
		if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &e); err != nil {
			t.Fatal(err)
		}
		if e.Error != "" {
			t.Fatal(e.Error)
		}
		if e.Type == "completed" && e.Card != nil {
			completed = true
			result = *e.Card
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	if !completed {
		t.Fatal("no completed SSE event")
	}
	return result
}
func TestHTTPWorkflowAndOwnership(t *testing.T) {
	server, client := newHTTPTest(t)
	base := server.URL
	var session cards.SessionView
	apiRequest(t, client, "POST", base+"/api/sessions", map[string]string{"title": "旅行规划"}, 201, &session)
	first := streamCard(t, client, base+"/api/sessions/"+session.ID+"/chat", cards.ChatInput{Operation: "deepen", ParentIDs: []string{session.RootID}, Question: "日本旅行", RequestID: "first"})
	child := streamCard(t, client, base+"/api/sessions/"+session.ID+"/chat", cards.ChatInput{Operation: "deepen", ParentIDs: []string{first.ID}, Question: "文化景点"})
	sibling := streamCard(t, client, base+"/api/sessions/"+session.ID+"/chat", cards.ChatInput{Operation: "branch", ParentIDs: []string{child.ID}, Question: "美食路线"})
	merged := streamCard(t, client, base+"/api/sessions/"+session.ID+"/chat", cards.ChatInput{Operation: "merge", ParentIDs: []string{child.ID, sibling.ID}, Question: "比较预算"})
	if len(merged.ParentIDs) != 2 {
		t.Fatal("merge parents missing")
	}
	var saved cards.Card
	apiRequest(t, client, "PATCH", base+"/api/sessions/"+session.ID+"/cards/"+merged.ID, map[string]bool{"favorite": true, "success": true}, 200, &saved)
	if !saved.Favorite || !saved.Success {
		t.Fatal("card flags not saved")
	}
	apiRequest(t, client, "GET", base+"/api/sessions/"+session.ID, nil, 200, &session)
	if len(session.Cards) < 5 {
		t.Fatalf("missing graph nodes: %d", len(session.Cards))
	}
	jar, _ := cookiejar.New(nil)
	other := &http.Client{Jar: jar}
	apiRequest(t, other, "GET", base+"/api/sessions/"+session.ID, nil, 404, nil)
	apiRequest(t, other, "DELETE", base+"/api/sessions/"+session.ID, nil, 404, nil)
	var share struct{ Token, URL string }
	apiRequest(t, client, "POST", base+"/api/sessions/"+session.ID+"/share", map[string]string{"card_id": merged.ID}, 201, &share)
	var public, imported cards.SessionView
	apiRequest(t, other, "GET", base+"/api/shares/"+share.Token, nil, 200, &public)
	apiRequest(t, other, "POST", base+"/api/shares/"+share.Token+"/import", map[string]string{}, 201, &imported)
	if imported.ID == session.ID {
		t.Fatal("import must create independent session")
	}
	apiRequest(t, other, "GET", base+"/api/sessions/"+imported.ID, nil, 200, nil)
	var copied cards.SessionView
	apiRequest(t, client, "POST", base+"/api/sessions/"+session.ID+"/copy", map[string]string{"card_id": child.ID}, 201, &copied)
	for _, card := range copied.Cards {
		if card.Question == sibling.Question {
			t.Fatal("copied unrelated branch")
		}
	}
	apiRequest(t, client, "GET", base+"/api/sessions/"+session.ID+"/export", nil, 200, nil)
	apiRequest(t, client, "PUT", base+"/api/preferences", cards.Preferences{Tone: "简洁", Interests: []string{"旅行"}}, 200, nil)
	var prefs cards.Preferences
	apiRequest(t, client, "GET", base+"/api/preferences", nil, 200, &prefs)
	if prefs.Tone != "简洁" {
		t.Fatal("preferences lost")
	}
}
func TestHTTPValidationAndLegacy(t *testing.T) {
	server, client := newHTTPTest(t)
	base := server.URL
	apiRequest(t, client, "POST", base+"/g/default", map[string]any{"ifFirst": true, "message": map[string]any{"content": []any{}}}, 400, nil)
	var response struct {
		Session cards.SessionId
		Version cards.Version
		Message struct{ Content struct{ Text []string } }
	}
	apiRequest(t, client, "POST", base+"/g/0", map[string]any{"ifFirst": true, "stream": false, "message": map[string]any{"content": []any{map[string]any{"text": []string{"你好"}}}}}, 200, &response)
	if response.Session.MainId == "" || response.Version.MainId == "" || len(response.Message.Content.Text) == 0 {
		t.Fatalf("legacy response incomplete: %+v", response)
	}
	req, _ := http.NewRequest("POST", base+"/api/sessions", strings.NewReader("{}"))
	req.Header.Set("Origin", "https://evil.example")
	res, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 403 {
		t.Fatal("cross-site mutation accepted")
	}
	for _, body := range []string{"{} {}", "{}broken", `{"title":`} {
		req, _ = http.NewRequest("POST", base+"/api/sessions", strings.NewReader(body))
		res, err = client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode != 400 {
			t.Fatalf("invalid body %q accepted", body)
		}
	}
}
