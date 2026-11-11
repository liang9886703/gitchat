package cards

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func connectionInput() ConnectionInput {
	return ConnectionInput{ModelConnection: ModelConnection{Name: "Private model", Protocol: "openai-chat", BaseURL: "https://example.com/v1", Model: "test-model", MaxTokens: 4096, TimeoutSeconds: 60, Stream: true, JSONMode: "native"}, APIKey: "test-secret-never-expose", Activate: true}
}
func TestModelConnectionsPersistenceAndIsolation(t *testing.T) {
	dir := t.TempDir()
	s := newTestServer(t, &recordingProvider{}, dir)
	input := connectionInput()
	view, err := s.SaveModelConnection("alice", input, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(view.Connections) != 1 || !view.Connections[0].HasKey || view.ActiveID != view.Connections[0].ID {
		t.Fatal(view)
	}
	id := view.ActiveID
	raw, _ := json.Marshal(view)
	disk, _ := os.ReadFile(filepath.Join(dir, "gitchat.json"))
	if strings.Contains(string(raw), input.APIKey) || strings.Contains(string(disk), input.APIKey) || strings.Contains(string(raw), "encrypted_key") {
		t.Fatal("secret leaked")
	}
	if len(s.ModelConnections("bob").Connections) != 0 {
		t.Fatal("owner isolation")
	}
	if _, err = s.SelectModel("bob", id, 0); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if _, err = s.SelectModel("alice", "demo", 0); !errors.Is(err, ErrConflict) {
		t.Fatal("stale revision accepted")
	}
	input.ModelConnection = view.Connections[0]
	input.APIKey = ""
	input.Revision = view.Revision
	view, err = s.SaveModelConnection("alice", input, false)
	if err != nil || !view.Connections[0].HasKey {
		t.Fatal(err)
	}
	input.BaseURL = "https://different.example/v1"
	input.Revision = view.Revision
	if _, err = s.SaveModelConnection("alice", input, false); !errors.Is(err, ErrInvalid) {
		t.Fatal("reused secret with new endpoint", err)
	}
	if _, err = s.DeleteModelConnection("alice", id, view.Revision); !errors.Is(err, ErrConflict) {
		t.Fatal("deleted active model")
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	reopened := newTestServer(t, &recordingProvider{}, dir)
	view = reopened.ModelConnections("alice")
	if view.ActiveID != id {
		t.Fatal(view)
	}
	p, err := reopened.state.providerForOwner("alice")
	if err != nil || p == nil {
		t.Fatal("persisted secret unreadable", err)
	}
	view, err = reopened.SelectModel("alice", "demo", view.Revision)
	if err != nil {
		t.Fatal(err)
	}
	view, err = reopened.DeleteModelConnection("alice", id, view.Revision)
	if err != nil || len(view.Connections) != 0 {
		t.Fatal(err)
	}
}
func TestClearKeyAndLostKey(t *testing.T) {
	dir := t.TempDir()
	s := newTestServer(t, &recordingProvider{}, dir)
	input := connectionInput()
	view, err := s.SaveModelConnection("alice", input, false)
	if err != nil {
		t.Fatal(err)
	}
	input.ModelConnection = view.Connections[0]
	input.APIKey = ""
	input.Revision = view.Revision
	input.ClearKey = true
	view, err = s.SaveModelConnection("alice", input, false)
	if err != nil || view.Connections[0].HasKey {
		t.Fatal(err)
	}
	input.ClearKey = false
	input.APIKey = "restored-secret"
	input.Revision = view.Revision
	_, err = s.SaveModelConnection("alice", input, false)
	if err != nil {
		t.Fatal(err)
	}
	s.Close()
	os.Remove(filepath.Join(dir, "model-key.bin"))
	reopened := newTestServer(t, &recordingProvider{}, dir)
	if _, err = reopened.state.providerForOwner("alice"); err == nil {
		t.Fatal("silently replaced lost encryption key")
	}
}
