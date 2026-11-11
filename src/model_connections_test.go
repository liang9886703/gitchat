package router

import "testing"

func TestModelConnectionsEntry(t *testing.T) {
	h, client := newHTTPTest(t)
	var got map[string]any
	apiRequest(t, client, "GET", h.URL+"/api/model-connections", nil, 200, &got)
	if got["active_id"] != "server" {
		t.Fatalf("missing server selection: %v", got)
	}
}

func TestModelConnectionHTTPWorkflow(t *testing.T) {
	h, client := newHTTPTest(t)
	input := map[string]any{"name": "My model", "protocol": "anthropic", "base_url": "https://api.anthropic.com/v1", "model": "user-model", "api_key": "never-return-this-key", "max_tokens": 4096, "timeout_seconds": 90, "stream": true, "json_mode": "native", "activate": true, "revision": 0}
	var view struct {
		ActiveID    string           `json:"active_id"`
		Revision    int              `json:"revision"`
		Connections []map[string]any `json:"connections"`
	}
	apiRequest(t, client, "POST", h.URL+"/api/model-connections", input, 201, &view)
	if view.ActiveID == "" || len(view.Connections) != 1 || view.Connections[0]["has_key"] != true || view.Connections[0]["api_key"] != nil || view.Connections[0]["encrypted_key"] != nil {
		t.Fatal(view)
	}
	var status map[string]any
	apiRequest(t, client, "GET", h.URL+"/api/config", nil, 200, &status)
	if status["protocol"] != "anthropic" || status["model"] != "user-model" || status["model_configured"] != true {
		t.Fatal(status)
	}
	id := view.ActiveID
	apiRequest(t, client, "DELETE", h.URL+"/api/model-connections/"+id, map[string]int{"revision": view.Revision}, 409, nil)
	apiRequest(t, client, "PUT", h.URL+"/api/model-selection", map[string]any{"id": "demo", "revision": 0}, 409, nil)
	apiRequest(t, client, "PUT", h.URL+"/api/model-selection", map[string]any{"id": "demo", "revision": view.Revision}, 200, &view)
	apiRequest(t, client, "DELETE", h.URL+"/api/model-connections/"+id, map[string]int{"revision": view.Revision}, 200, &view)
	if len(view.Connections) != 0 {
		t.Fatal(view)
	}
	apiRequest(t, client, "GET", h.URL+"/api/config", nil, 200, &status)
	if status["mode"] != "demo" || status["model_configured"] != false {
		t.Fatal(status)
	}
}
