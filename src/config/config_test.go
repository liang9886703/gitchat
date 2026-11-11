package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEnvironmentAndLegacyConfiguration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	body := `namespace:
  - name: default
    chatbotId: "0"
    chatbot:
      baseURL: https://example.com/v1
      model: configured
      maxTokens: 1000
chattemplate:
  - name: default
    template:
      sysprompt: help the user
`
	if err := os.WriteFile(path, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GITCHAT_CONFIG", path)
	t.Setenv("GITCHAT_MODE", "")
	t.Setenv("OPENAI_API_KEY_0", "legacy-secret")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("OPENAI_MODEL", "override")
	t.Setenv("DATA_DIR", t.TempDir())
	cfg, err := GetConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Mode != "openai" || cfg.Namespace[0].Chatbot.Model != "override" || cfg.Namespace[0].Chatbot.APIKey != "legacy-secret" {
		t.Fatalf("incorrect config %+v", cfg)
	}
}
func TestDemoWithoutKeyAndInvalidFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	_ = os.WriteFile(path, []byte("mode: demo\n"), 0600)
	t.Setenv("GITCHAT_CONFIG", path)
	t.Setenv("GITCHAT_MODE", "")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("OPENAI_API_KEY_0", "")
	cfg, err := GetConfig()
	if err != nil || cfg.Mode != "demo" {
		t.Fatalf("demo config %v %v", cfg, err)
	}
	t.Setenv("GITCHAT_MODE", "openai")
	if _, err = GetConfig(); err == nil {
		t.Fatal("missing key accepted")
	}
	t.Setenv("GITCHAT_CONFIG", path+".missing")
	if _, err = GetConfig(); err == nil {
		t.Fatal("missing config file accepted")
	}
}
