package cards

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"gitchat/src/config"
	"gitchat/src/modelServer"
)

type ModelConnection struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	Protocol       string `json:"protocol"`
	BaseURL        string `json:"base_url"`
	Model          string `json:"model"`
	MaxTokens      int    `json:"max_tokens"`
	TimeoutSeconds int    `json:"timeout_seconds"`
	Stream         bool   `json:"stream"`
	JSONMode       string `json:"json_mode"`
	TokenParameter string `json:"token_parameter"`
	HasKey         bool   `json:"has_key"`
}
type storedConnection struct {
	ModelConnection
	Secret     string `json:"encrypted_key,omitempty"`
	AllowLocal bool   `json:"allow_local,omitempty"`
}
type ModelSettings struct {
	ActiveID    string             `json:"active_id"`
	Revision    int                `json:"revision"`
	Connections []storedConnection `json:"connections"`
}
type ModelSettingsView struct {
	ActiveID    string            `json:"active_id"`
	Revision    int               `json:"revision"`
	Connections []ModelConnection `json:"connections"`
	Server      ModelStatus       `json:"server"`
}
type ModelStatus struct {
	Mode       string `json:"mode"`
	Model      string `json:"model"`
	Protocol   string `json:"protocol"`
	Name       string `json:"connection_name"`
	Configured bool   `json:"model_configured"`
}
type ConnectionInput struct {
	ModelConnection
	APIKey   string `json:"api_key"`
	ClearKey bool   `json:"clear_key"`
	Revision int    `json:"revision"`
	Activate bool   `json:"activate"`
}

func (s *serverState) settings(owner string) ModelSettings {
	value := s.db.Models[owner]
	if value.ActiveID == "" {
		value.ActiveID = "server"
	}
	if value.Connections == nil {
		value.Connections = []storedConnection{}
	}
	return value
}
func (s *serverState) serverModelStatus() ModelStatus {
	status := ModelStatus{Mode: "demo", Name: "演示模式"}
	if s.baseConfig.Mode == "openai" || s.baseConfig.Mode == "" && len(s.baseConfig.Namespace) > 0 && s.baseConfig.Namespace[0].Chatbot.APIKey != "" {
		status.Mode = "openai"
		status.Configured = true
		status.Name = "服务端默认"
		if len(s.baseConfig.Namespace) > 0 {
			bot := s.baseConfig.Namespace[0].Chatbot
			status.Model = bot.Model
			status.Protocol = bot.Protocol
			if status.Protocol == "" {
				status.Protocol = "openai-chat"
			}
		}
	}
	return status
}
func (cs *CardServer) ModelStatus(owner string) ModelStatus {
	s := cs.state
	s.mu.Lock()
	defer s.mu.Unlock()
	settings := s.settings(owner)
	if settings.ActiveID == "server" {
		return s.serverModelStatus()
	}
	if settings.ActiveID == "demo" {
		return ModelStatus{Mode: "demo", Name: "演示模式"}
	}
	for _, c := range settings.Connections {
		if c.ID == settings.ActiveID {
			return ModelStatus{Mode: "openai", Name: c.Name, Protocol: c.Protocol, Model: c.Model, Configured: true}
		}
	}
	return ModelStatus{Mode: "unavailable", Name: "模型配置不可用"}
}
func (s *serverState) modelSettingsView(owner string) ModelSettingsView {
	settings := s.settings(owner)
	view := ModelSettingsView{ActiveID: settings.ActiveID, Revision: settings.Revision, Connections: []ModelConnection{}, Server: s.serverModelStatus()}
	for _, connection := range settings.Connections {
		public := connection.ModelConnection
		public.HasKey = connection.Secret != ""
		view.Connections = append(view.Connections, public)
	}
	return view
}
func (cs *CardServer) ModelConnections(owner string) ModelSettingsView {
	s := cs.state
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.modelSettingsView(owner)
}

func (s *serverState) key(create bool) ([]byte, error) {
	if len(s.secretKey) == 32 {
		return s.secretKey, nil
	}
	if s.file != "" {
		path := filepath.Join(filepath.Dir(s.file), "model-key.bin")
		key, err := os.ReadFile(path)
		if err == nil {
			if len(key) != 32 {
				return nil, errors.New("模型密钥文件损坏")
			}
			s.secretKey = key
			return key, nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return nil, errors.New("无法读取模型密钥文件")
		}
		// Never create a replacement key when existing credentials need decryption.
		for _, settings := range s.db.Models {
			for _, c := range settings.Connections {
				if c.Secret != "" {
					return nil, errors.New("模型密钥文件缺失，请恢复 model-key.bin 后重试")
				}
			}
		}
		if !create {
			return nil, errors.New("模型密钥文件缺失")
		}
		key = make([]byte, 32)
		if _, err = rand.Read(key); err != nil {
			return nil, err
		}
		file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			return nil, err
		}
		if _, err = file.Write(key); err == nil {
			err = file.Sync()
		}
		closeErr := file.Close()
		if err != nil {
			return nil, err
		}
		if closeErr != nil {
			return nil, closeErr
		}
		s.secretKey = key
		return key, nil
	}
	if !create {
		return nil, errors.New("model key is unavailable")
	}
	s.secretKey = make([]byte, 32)
	_, err := rand.Read(s.secretKey)
	return s.secretKey, err
}
func (s *serverState) seal(owner, id, key string) (string, error) {
	if key == "" {
		return "", nil
	}
	secret, err := s.key(true)
	if err != nil {
		return "", err
	}
	block, _ := aes.NewCipher(secret)
	aead, _ := cipher.NewGCM(block)
	nonce := make([]byte, aead.NonceSize())
	if _, err = rand.Read(nonce); err != nil {
		return "", err
	}
	sealed := aead.Seal(nonce, nonce, []byte(key), []byte(owner+":"+id))
	return base64.StdEncoding.EncodeToString(sealed), nil
}
func (s *serverState) unseal(owner string, c storedConnection) (string, error) {
	if c.Secret == "" {
		return "", nil
	}
	key, err := s.key(false)
	if err != nil {
		return "", err
	}
	block, _ := aes.NewCipher(key)
	aead, _ := cipher.NewGCM(block)
	sealed, err := base64.StdEncoding.DecodeString(c.Secret)
	if err != nil || len(sealed) < aead.NonceSize() {
		return "", errors.New("无法解密模型密钥，请重新填写")
	}
	raw, err := aead.Open(nil, sealed[:aead.NonceSize()], sealed[aead.NonceSize():], []byte(owner+":"+c.ID))
	if err != nil {
		return "", errors.New("无法解密模型密钥，请重新填写")
	}
	return string(raw), nil
}
func (s *serverState) prepareConnection(owner string, input ConnectionInput, allowLocal, requireModel bool) (storedConnection, string, error) {
	c := storedConnection{ModelConnection: input.ModelConnection, AllowLocal: allowLocal}
	c.Name = strings.TrimSpace(c.Name)
	c.Model = strings.TrimSpace(c.Model)
	c.Protocol = strings.TrimSpace(c.Protocol)
	invalid := func(message string) (storedConnection, string, error) {
		return c, "", fmt.Errorf("%w: %s", ErrInvalid, message)
	}
	if c.Protocol == "" || !modelServer.ValidProtocol(c.Protocol) {
		return invalid("请选择支持的模型协议")
	}
	if utf8.RuneCountInString(c.Name) > 60 || c.Name == "" {
		return invalid("连接名称须为 1–60 字")
	}
	if requireModel && c.Model == "" || len(c.Model) > 200 || strings.ContainsAny(c.Model, "\r\n?#") {
		return invalid("请填写有效模型 ID")
	}
	if c.MaxTokens < 128 || c.MaxTokens > 65536 {
		return invalid("输出上限须为 128–65536 tokens")
	}
	if c.TimeoutSeconds < 5 || c.TimeoutSeconds > 120 {
		return invalid("超时须为 5–120 秒")
	}
	if c.JSONMode != "native" && c.JSONMode != "prompt" {
		return invalid("请选择 JSON 或提示词兼容模式")
	}
	if c.TokenParameter != "" && c.TokenParameter != "max_tokens" && c.TokenParameter != "max_completion_tokens" {
		return invalid("输出参数无效")
	}
	endpoint, err := modelServer.NormalizeEndpoint(c.BaseURL, c.Protocol, allowLocal)
	if err != nil {
		return invalid(err.Error())
	}
	c.BaseURL = endpoint
	key := strings.TrimSpace(input.APIKey)
	if len(key) > 8192 || strings.ContainsAny(key, "\r\n") {
		return invalid("密钥格式无效")
	}
	if c.ID != "" {
		found := false
		for _, old := range s.settings(owner).Connections {
			if old.ID != c.ID {
				continue
			}
			found = true
			if key == "" && !input.ClearKey {
				if old.Secret != "" && (old.BaseURL != c.BaseURL || old.Protocol != c.Protocol) {
					return invalid("更换地址或协议后，请重新填写密钥，或明确清除密钥")
				}
				key, err = s.unseal(owner, old)
				if err != nil {
					return c, "", err
				}
			}
		}
		if !found {
			return c, "", ErrNotFound
		}
	} else {
		c.ID = newID()
	}
	if input.ClearKey {
		key = ""
	}
	c.HasKey = key != ""
	return c, key, nil
}
func (s *serverState) connectionConfig(c storedConnection, key string) *config.Config {
	cfg := s.baseConfig
	cfg.Mode = "openai"
	cfg.TimeoutSeconds = c.TimeoutSeconds
	cfg.Namespace = []config.Namespace{{Name: "default", ChatbotID: "0", Chatbot: config.Chatbot{Protocol: c.Protocol, BaseURL: c.BaseURL, Model: c.Model, APIKey: key, NoAuth: key == "", MaxTokens: c.MaxTokens, IsStream: c.Stream, JSONMode: c.JSONMode, TokenParameter: c.TokenParameter}}}
	cfg.Routing.StrongModel = ""
	cfg.Routing.FallbackModel = ""
	cfg.Routing.Topics = nil
	cfg.Routing.Retries = 0
	return &cfg
}
func (s *serverState) providerForOwner(owner string) (modelServer.Provider, error) {
	settings := s.settings(owner)
	if settings.ActiveID == "server" {
		return s.provider, nil
	}
	if settings.ActiveID == "demo" {
		cfg := s.baseConfig
		cfg.Mode = "demo"
		return modelServer.NewProvider(&cfg)
	}
	for _, c := range settings.Connections {
		if c.ID == settings.ActiveID {
			key, err := s.unseal(owner, c)
			if err != nil {
				return nil, err
			}
			return modelServer.NewManagedProvider(s.connectionConfig(c, key), c.AllowLocal)
		}
	}
	return nil, errors.New("当前模型连接已丢失，请重新选择")
}
func (cs *CardServer) SaveModelConnection(owner string, input ConnectionInput, allowLocal bool) (ModelSettingsView, error) {
	s := cs.state
	s.mu.Lock()
	defer s.mu.Unlock()
	settings := s.settings(owner)
	if s.closed || settings.Revision != input.Revision {
		return ModelSettingsView{}, ErrConflict
	}
	c, key, err := s.prepareConnection(owner, input, allowLocal, true)
	if err != nil {
		return ModelSettingsView{}, err
	}
	if input.ID == "" && len(settings.Connections) >= 12 {
		return ModelSettingsView{}, fmt.Errorf("%w: 最多保存 12 个连接", ErrInvalid)
	}
	c.Secret, err = s.seal(owner, c.ID, key)
	if err != nil {
		return ModelSettingsView{}, err
	}
	err = s.mutate(func() error {
		settings = clone(settings)
		replaced := false
		for i, old := range settings.Connections {
			if old.ID == c.ID {
				settings.Connections[i] = c
				replaced = true
			}
		}
		if !replaced {
			settings.Connections = append(settings.Connections, c)
		}
		if input.Activate {
			settings.ActiveID = c.ID
		}
		settings.Revision++
		s.db.Models[owner] = settings
		return nil
	})
	return s.modelSettingsView(owner), err
}
func (cs *CardServer) SelectModel(owner, id string, revision int) (ModelSettingsView, error) {
	s := cs.state
	s.mu.Lock()
	defer s.mu.Unlock()
	settings := s.settings(owner)
	if s.closed || revision != settings.Revision {
		return ModelSettingsView{}, ErrConflict
	}
	found := id == "server" || id == "demo"
	for _, c := range settings.Connections {
		found = found || c.ID == id
	}
	if !found {
		return ModelSettingsView{}, ErrNotFound
	}
	err := s.mutate(func() error { settings.ActiveID = id; settings.Revision++; s.db.Models[owner] = settings; return nil })
	return s.modelSettingsView(owner), err
}
func (cs *CardServer) DeleteModelConnection(owner, id string, revision int) (ModelSettingsView, error) {
	s := cs.state
	s.mu.Lock()
	defer s.mu.Unlock()
	settings := s.settings(owner)
	if s.closed || revision != settings.Revision {
		return ModelSettingsView{}, ErrConflict
	}
	if settings.ActiveID == id {
		return ModelSettingsView{}, fmt.Errorf("%w: 请先切换到其他模型，再删除当前连接", ErrConflict)
	}
	index := -1
	for i, c := range settings.Connections {
		if c.ID == id {
			index = i
		}
	}
	if index < 0 {
		return ModelSettingsView{}, ErrNotFound
	}
	err := s.mutate(func() error {
		settings = clone(settings)
		settings.Connections = append(settings.Connections[:index], settings.Connections[index+1:]...)
		settings.Revision++
		s.db.Models[owner] = settings
		return nil
	})
	return s.modelSettingsView(owner), err
}
func (cs *CardServer) ProbeModel(ctx context.Context, owner string, input ConnectionInput, allowLocal, list bool) (any, error) {
	s := cs.state
	s.mu.Lock()
	if input.Revision != s.settings(owner).Revision {
		s.mu.Unlock()
		return nil, ErrConflict
	}
	c, key, err := s.prepareConnection(owner, input, allowLocal, !list)
	s.mu.Unlock()
	if err != nil {
		return nil, err
	}
	cfg := s.connectionConfig(c, key)
	if list {
		return modelServer.ListModels(ctx, cfg, allowLocal)
	}
	p, err := modelServer.NewManagedProvider(cfg, allowLocal)
	if err != nil {
		return nil, err
	}
	start := time.Now()
	result, err := p.Generate(ctx, modelServer.Request{Question: "请用一句话说明如何把一个问题拆成可逐步探索的小问题。", Operation: "deepen", Depth: 1}, nil)
	if err != nil {
		return nil, err
	}
	return map[string]any{"ok": true, "model": result.Model, "protocol": c.Protocol, "latency_ms": time.Since(start).Milliseconds(), "answer": result.Answer}, nil
}
