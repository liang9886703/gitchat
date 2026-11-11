package main

import (
	"gitchat/src/config"
	"os"
	"path/filepath"
	"testing"
)

func TestGetConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("mode: demo\n"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GITCHAT_CONFIG", path)
	cfg, err := config.GetConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Mode != "demo" || cfg.ListenAddr == "" {
		t.Fatalf("invalid defaults: mode=%q addr=%q", cfg.Mode, cfg.ListenAddr)
	}
}
