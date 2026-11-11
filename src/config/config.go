package config

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

type Chatbot struct {
	BaseURL   string `yaml:"baseURL" json:"base_url"`
	Model     string `yaml:"model" json:"model"`
	MaxTokens int    `yaml:"maxTokens" json:"max_tokens"`
	IsStream  bool   `yaml:"isStream" json:"is_stream"`
	APIKey    string `yaml:"-" json:"-"`
}
type Namespace struct {
	Name      string  `yaml:"name"`
	ChatbotID string  `yaml:"chatbotId"`
	Chatbot   Chatbot `yaml:"chatbot"`
}
type Template struct {
	Sysprompt      string   `yaml:"sysprompt"`
	Userprompt     string   `yaml:"userprompt"`
	Tools          []string `yaml:"tools"`
	Responseformat string   `yaml:"responseformat"`
}
type Chattemplate struct {
	Name     string   `yaml:"name"`
	Template Template `yaml:"template"`
}
type TopicRoute struct {
	Keywords []string `yaml:"keywords"`
	Model    string   `yaml:"model"`
}
type Routing struct {
	StrongModel    string       `yaml:"strongModel"`
	FallbackModel  string       `yaml:"fallbackModel"`
	DeepAfter      int          `yaml:"deepAfter"`
	ContextChars   int          `yaml:"contextChars"`
	MaxAnswerChars int          `yaml:"maxAnswerChars"`
	Retries        int          `yaml:"retries"`
	Topics         []TopicRoute `yaml:"topics"`
}
type Privacy struct {
	Redact     bool `yaml:"redact"`
	ModelCheck bool `yaml:"modelCheck"`
}
type MCPServer struct {
	URL      string   `yaml:"url"`
	TokenEnv string   `yaml:"tokenEnv"`
	Tools    []string `yaml:"tools"`
}
type Config struct {
	Namespace      []Namespace    `yaml:"namespace"`
	Chattemplate   []Chattemplate `yaml:"chattemplate"`
	Others         string         `yaml:"others"`
	DataDir        string         `yaml:"dataDir"`
	ListenAddr     string         `yaml:"listenAddr"`
	Mode           string         `yaml:"mode"`
	TimeoutSeconds int            `yaml:"timeoutSeconds"`
	Routing        Routing        `yaml:"routing"`
	Privacy        Privacy        `yaml:"privacy"`
	KnowledgeDir   string         `yaml:"knowledgeDir"`
	MCPServers     []MCPServer    `yaml:"mcpServers"`
}

func Defaults() *Config {
	return &Config{
		DataDir:        "./data",
		ListenAddr:     "127.0.0.1:8002",
		TimeoutSeconds: 90,
		Namespace: []Namespace{{
			Name:      "default",
			ChatbotID: "0",
			Chatbot: Chatbot{
				BaseURL:   "https://api.openai.com/v1",
				Model:     "gpt-4o-mini",
				MaxTokens: 2400,
				IsStream:  true,
			},
		}},
		Chattemplate: []Chattemplate{{
			Name: "default",
			Template: Template{
				Sysprompt: "帮助用户逐步探索问题。每张卡片聚焦一个问题，给出简明回答和可自主选择的思考方向。",
			},
		}},
		Routing: Routing{
			DeepAfter:      4,
			ContextChars:   12000,
			MaxAnswerChars: 1200,
			Retries:        1,
		},
		Privacy: Privacy{Redact: true},
	}
}

// GetConfig loads an optional YAML configuration and applies environment overrides.
func GetConfig() (*Config, error) {
	cfg := Defaults()
	path := os.Getenv("GITCHAT_CONFIG")
	if path != "" {
		body, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read configuration: %w", err)
		}
		if err = yaml.Unmarshal(body, cfg); err != nil {
			return nil, fmt.Errorf("parse configuration: %w", err)
		}
	} else {
		for _, candidate := range []string{"config.yaml", "config/config.yaml", "/work/config/config.yaml"} {
			body, err := os.ReadFile(candidate)
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			if err != nil {
				return nil, err
			}
			if err = yaml.Unmarshal(body, cfg); err != nil {
				return nil, err
			}
			break
		}
	}
	if len(cfg.Namespace) == 0 {
		return nil, errors.New("at least one namespace is required")
	}
	for i := range cfg.Namespace {
		ns := &cfg.Namespace[i]
		ns.Chatbot.APIKey = os.Getenv("OPENAI_API_KEY_" + ns.ChatbotID)
		if ns.Chatbot.APIKey == "" {
			ns.Chatbot.APIKey = os.Getenv("OPENAI_API_KEY")
		}
		if v := os.Getenv("OPENAI_BASE_URL"); v != "" {
			ns.Chatbot.BaseURL = v
		}
		if v := os.Getenv("OPENAI_MODEL"); v != "" {
			ns.Chatbot.Model = v
		}
		if ns.Chatbot.MaxTokens == 0 {
			ns.Chatbot.MaxTokens = 2400
		}
	}
	if v := os.Getenv("GITCHAT_MODE"); v != "" {
		cfg.Mode = v
	}
	if v := os.Getenv("DATA_DIR"); v != "" {
		cfg.DataDir = v
	}
	if v := os.Getenv("PORT"); v != "" {
		cfg.ListenAddr = "127.0.0.1:" + v
	}
	if v := os.Getenv("GITCHAT_ADDR"); v != "" {
		cfg.ListenAddr = v
	}
	if v := os.Getenv("GITCHAT_KNOWLEDGE_DIR"); v != "" {
		cfg.KnowledgeDir = v
	}
	if cfg.Mode == "" {
		cfg.Mode = "demo"
		if cfg.Namespace[0].Chatbot.APIKey != "" {
			cfg.Mode = "openai"
		}
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}
func (c *Config) Validate() error {
	if c == nil {
		return errors.New("configuration is nil")
	}
	if c.Mode != "demo" && c.Mode != "openai" {
		return errors.New("mode must be demo or openai")
	}
	if len(c.Namespace) == 0 {
		return errors.New("at least one namespace is required")
	}
	if c.TimeoutSeconds <= 0 || c.TimeoutSeconds > 120 {
		return errors.New("timeoutSeconds must be between 1 and 120")
	}
	if c.Routing.Retries < 0 || c.Routing.Retries > 3 {
		return errors.New("routing.retries must be between 0 and 3")
	}
	if c.Routing.MaxAnswerChars < 120 || c.Routing.MaxAnswerChars > 8000 {
		return errors.New("routing.maxAnswerChars must be between 120 and 8000")
	}
	if c.Routing.ContextChars < 1000 || c.Routing.ContextChars > 200000 {
		return errors.New("routing.contextChars must be between 1000 and 200000")
	}
	if c.DataDir == "" {
		return errors.New("dataDir is required")
	}
	for _, ns := range c.Namespace {
		if strings.TrimSpace(ns.Name) == "" || strings.TrimSpace(ns.ChatbotID) == "" {
			return errors.New("namespace name and chatbotId are required")
		}
		if c.Mode == "openai" {
			if ns.Chatbot.APIKey == "" {
				return fmt.Errorf("missing API key for chatbot %s", ns.ChatbotID)
			}
			if err := validURL(ns.Chatbot.BaseURL); err != nil {
				return fmt.Errorf("baseURL: %w", err)
			}
			if ns.Chatbot.Model == "" {
				return errors.New("model is required")
			}
		}
	}
	if len(c.Chattemplate) == 0 {
		return errors.New("at least one chat template is required")
	}
	if c.KnowledgeDir != "" {
		info, err := os.Stat(filepath.Clean(c.KnowledgeDir))
		if err != nil || !info.IsDir() {
			return errors.New("knowledgeDir must be an existing directory")
		}
	}
	for _, server := range c.MCPServers {
		if err := validURL(server.URL); err != nil {
			return fmt.Errorf("MCP URL: %w", err)
		}
		if len(server.Tools) == 0 {
			return errors.New("MCP server requires an explicit tools allowlist")
		}
	}
	return nil
}
func validURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil {
		return errors.New("expected an http(s) URL without embedded credentials")
	}
	return nil
}
