package modelServer

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"

	"gitchat/src/config"
)

func ValidProtocol(protocol string) bool {
	switch protocol {
	case "", "openai-chat", "openai-responses", "anthropic", "gemini", "ollama":
		return true
	}
	return false
}
func rejectRedirect(_ *http.Request, _ []*http.Request) error {
	return errors.New("model endpoint redirects are not allowed")
}
func protocolOf(bot config.Chatbot) string {
	if bot.Protocol == "" {
		return "openai-chat"
	}
	return bot.Protocol
}
func authorize(req *http.Request, bot config.Chatbot) {
	if bot.APIKey != "" {
		switch protocolOf(bot) {
		case "anthropic":
			req.Header.Set("x-api-key", bot.APIKey)
		case "gemini":
			req.Header.Set("x-goog-api-key", bot.APIKey)
		default:
			req.Header.Set("Authorization", "Bearer "+bot.APIKey)
		}
	}
	if protocolOf(bot) == "anthropic" {
		req.Header.Set("anthropic-version", "2023-06-01")
	}
}

// Only the leading application system message is privileged. Later system messages
// contain summarized conversation history and are passed as user context.
func separateSystem(messages []Message) (string, []Message) {
	system := ""
	history := []Message{}
	for i, message := range messages {
		if i == 0 && message.Role == "system" {
			system = message.Content
			continue
		}
		if message.Role != "assistant" {
			message.Role = "user"
		}
		if len(history) > 0 && history[len(history)-1].Role == message.Role {
			history[len(history)-1].Content += "\n\n" + message.Content
		} else {
			history = append(history, message)
		}
	}
	return system, history
}
func (p *provider) complete(ctx context.Context, model string, messages []Message, structured bool) (string, error) {
	bot := p.cfg.Namespace[0].Chatbot
	protocol := protocolOf(bot)
	tokens := bot.MaxTokens
	if tokens <= 0 {
		tokens = 2400
	}
	nativeJSON := structured && bot.JSONMode != "prompt"
	endpoint := strings.TrimRight(bot.BaseURL, "/")
	body := map[string]any{"model": model, "stream": bot.IsStream}
	switch protocol {
	case "openai-chat":
		endpoint += "/chat/completions"
		body["messages"] = messages
		param := bot.TokenParameter
		if param == "" {
			param = "max_tokens"
		}
		body[param] = tokens
		if nativeJSON {
			body["response_format"] = map[string]string{"type": "json_object"}
		}
	case "openai-responses":
		endpoint += "/responses"
		body["input"] = messages
		body["store"] = false
		body["max_output_tokens"] = tokens
		if nativeJSON {
			body["text"] = map[string]any{"format": map[string]string{"type": "json_object"}}
		}
	case "anthropic":
		endpoint += "/messages"
		system, history := separateSystem(messages)
		body["system"] = system
		body["messages"] = history
		body["max_tokens"] = tokens
	case "gemini":
		method := "generateContent"
		if bot.IsStream {
			method = "streamGenerateContent"
		}
		endpoint += "/models/" + url.PathEscape(strings.TrimPrefix(model, "models/")) + ":" + method
		if bot.IsStream {
			endpoint += "?alt=sse"
		}
		system, history := separateSystem(messages)
		contents := []any{}
		for _, m := range history {
			role := "user"
			if m.Role == "assistant" {
				role = "model"
			}
			contents = append(contents, map[string]any{"role": role, "parts": []any{map[string]string{"text": m.Content}}})
		}
		generation := map[string]any{"maxOutputTokens": tokens}
		if nativeJSON {
			generation["responseMimeType"] = "application/json"
		}
		body = map[string]any{"systemInstruction": map[string]any{"parts": []any{map[string]string{"text": system}}}, "contents": contents, "generationConfig": generation}
	case "ollama":
		endpoint += "/api/chat"
		body["messages"] = messages
		body["options"] = map[string]int{"num_predict": tokens}
		if nativeJSON {
			body["format"] = "json"
		}
	default:
		return "", errors.New("unsupported model protocol")
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return "", errors.New("invalid model endpoint")
	}
	authorize(req, bot)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream, application/x-ndjson")
	res, err := p.client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		return "", errors.New("无法连接模型服务，请检查地址、网络和证书")
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return "", &providerStatusError{res.StatusCode}
	}
	reader := io.LimitReader(res.Body, 2<<20)
	if strings.Contains(res.Header.Get("Content-Type"), "text/event-stream") {
		if protocol == "openai-chat" {
			return readCompletionSSE(reader)
		}
		return readProtocolSSE(reader, protocol)
	}
	if protocol == "ollama" && bot.IsStream {
		return readOllama(reader)
	}
	raw, err := io.ReadAll(reader)
	if err != nil {
		return "", err
	}
	if len(raw) >= 2<<20 {
		return "", errors.New("model response too large")
	}
	value, _, err := decodeEnvelope(raw, protocol, false)
	return value, err
}

type textBlock struct {
	Type    string `json:"type"`
	Text    string `json:"text"`
	Thought bool   `json:"thought"`
}
type envelope struct {
	Type       string          `json:"type"`
	Error      json.RawMessage `json:"error"`
	Status     string          `json:"status"`
	StopReason string          `json:"stop_reason"`
	Content    []textBlock     `json:"content"`
	Output     []struct {
		Type    string      `json:"type"`
		Content []textBlock `json:"content"`
	} `json:"output"`
	Candidates []struct {
		Content struct {
			Parts []textBlock `json:"parts"`
		} `json:"content"`
		FinishReason string `json:"finishReason"`
	} `json:"candidates"`
	PromptFeedback struct {
		BlockReason string `json:"blockReason"`
	} `json:"promptFeedback"`
	Message    Message `json:"message"`
	Done       bool    `json:"done"`
	DoneReason string  `json:"done_reason"`
	Choices    []struct {
		Message      Message `json:"message"`
		FinishReason string  `json:"finish_reason"`
	} `json:"choices"`
}

func decodeEnvelope(raw []byte, protocol string, streaming bool) (string, bool, error) {
	var e envelope
	if json.Unmarshal(raw, &e) != nil {
		return "", false, errors.New("invalid provider response")
	}
	if len(e.Error) > 0 && string(e.Error) != "null" || e.Type == "error" {
		return "", false, errors.New("provider reported an error")
	}
	var out strings.Builder
	done := false
	switch protocol {
	case "openai-chat":
		if len(e.Choices) == 0 {
			return "", false, errors.New("provider returned no choices")
		}
		c := e.Choices[0]
		if c.FinishReason != "" && c.FinishReason != "stop" {
			return "", false, errors.New("模型未完成回答，请检查输出上限或内容限制")
		}
		out.WriteString(c.Message.Content)
		done = true
	case "openai-responses":
		if e.Status != "completed" {
			return "", false, errors.New("Responses response did not complete")
		}
		for _, item := range e.Output {
			if item.Type == "message" {
				for _, block := range item.Content {
					if block.Type == "refusal" {
						return "", false, errors.New("model refused the request")
					}
					if block.Type == "output_text" {
						out.WriteString(block.Text)
					}
				}
			}
		}
		done = true
	case "anthropic":
		if e.StopReason != "end_turn" && e.StopReason != "stop_sequence" {
			return "", false, errors.New("Claude 未完成回答，请检查输出上限或内容限制")
		}
		for _, b := range e.Content {
			if b.Type == "text" {
				out.WriteString(b.Text)
			}
		}
		done = true
	case "gemini":
		if e.PromptFeedback.BlockReason != "" {
			return "", false, errors.New("Gemini blocked the request")
		}
		if len(e.Candidates) > 0 {
			c := e.Candidates[0]
			if c.FinishReason != "" && c.FinishReason != "STOP" {
				return "", false, errors.New("Gemini 未完成回答，请检查输出上限或内容限制")
			}
			for _, b := range c.Content.Parts {
				if !b.Thought {
					out.WriteString(b.Text)
				}
			}
			done = c.FinishReason == "STOP"
		}
		if !streaming && !done {
			return "", false, errors.New("Gemini returned no completed candidate")
		}
	case "ollama":
		if e.DoneReason == "length" {
			return "", false, errors.New("Ollama response exceeded token budget")
		}
		out.WriteString(e.Message.Content)
		done = e.Done
		if !streaming && !done {
			return "", false, errors.New("Ollama response did not complete")
		}
	}
	if !streaming && strings.TrimSpace(out.String()) == "" {
		return "", false, errors.New("model returned no answer text")
	}
	return out.String(), done, nil
}
func readProtocolSSE(reader io.Reader, protocol string) (string, error) {
	var out strings.Builder
	done := false
	stopped := false
	err := scanSSE(reader, func(raw []byte) error {
		var event struct {
			Type     string          `json:"type"`
			Delta    json.RawMessage `json:"delta"`
			Response json.RawMessage `json:"response"`
		}
		if json.Unmarshal(raw, &event) != nil {
			return errors.New("invalid provider stream event")
		}
		if event.Type == "error" || event.Type == "response.failed" || event.Type == "response.incomplete" {
			return errors.New("provider stream failed or was truncated")
		}
		switch protocol {
		case "openai-responses":
			if event.Type == "response.output_text.delta" {
				var delta string
				if json.Unmarshal(event.Delta, &delta) != nil {
					return errors.New("invalid text delta")
				}
				out.WriteString(delta)
			}
			if event.Type == "response.refusal.delta" {
				return errors.New("model refused the request")
			}
			if event.Type == "response.completed" {
				text, _, err := decodeEnvelope(event.Response, protocol, false)
				if err != nil {
					return err
				}
				if out.Len() == 0 {
					out.WriteString(text)
				}
				done = true
			}
		case "anthropic":
			var delta struct {
				Type       string `json:"type"`
				Text       string `json:"text"`
				StopReason string `json:"stop_reason"`
			}
			if len(event.Delta) > 0 && json.Unmarshal(event.Delta, &delta) != nil {
				return errors.New("invalid Claude delta")
			}
			if event.Type == "content_block_delta" && delta.Type == "text_delta" {
				out.WriteString(delta.Text)
			}
			if event.Type == "message_delta" {
				if delta.StopReason != "end_turn" && delta.StopReason != "stop_sequence" {
					return errors.New("Claude 未完成回答，请检查输出上限或内容限制")
				}
				stopped = true
			}
			if event.Type == "message_stop" {
				done = stopped
			}
		case "gemini":
			value, finished, err := decodeEnvelope(raw, protocol, true)
			if err != nil {
				return err
			}
			out.WriteString(value)
			done = done || finished
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	if !done || out.Len() == 0 {
		return "", errors.New("provider stream ended before completion")
	}
	return out.String(), nil
}
func scanSSE(reader io.Reader, consume func([]byte) error) error {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 4096), 1<<20)
	data := []string{}
	flush := func() error {
		if len(data) == 0 {
			return nil
		}
		value := strings.Join(data, "\n")
		data = nil
		return consume([]byte(value))
	}
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			if err := flush(); err != nil {
				return err
			}
		} else if strings.HasPrefix(line, "data:") {
			data = append(data, strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " "))
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	return flush()
}
func readOllama(reader io.Reader) (string, error) {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 4096), 1<<20)
	var out strings.Builder
	done := false
	for scanner.Scan() {
		if strings.TrimSpace(scanner.Text()) == "" {
			continue
		}
		value, finished, err := decodeEnvelope(scanner.Bytes(), "ollama", true)
		if err != nil {
			return "", err
		}
		out.WriteString(value)
		done = done || finished
	}
	if err := scanner.Err(); err != nil {
		return "", err
	}
	if !done || out.Len() == 0 {
		return "", errors.New("Ollama stream ended before completion")
	}
	return out.String(), nil
}

type ModelList struct {
	Models []string `json:"models"`
	More   bool     `json:"more"`
}

func ListModels(ctx context.Context, cfg *config.Config, allowLocal bool) (ModelList, error) {
	p, err := NewManagedProvider(cfg, allowLocal)
	if err != nil {
		return ModelList{}, err
	}
	bot := cfg.Namespace[0].Chatbot
	endpoint := strings.TrimRight(bot.BaseURL, "/") + "/models"
	if protocolOf(bot) == "ollama" {
		endpoint = strings.TrimRight(bot.BaseURL, "/") + "/api/tags"
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	authorize(req, bot)
	res, err := p.(*provider).client.Do(req)
	if err != nil {
		return ModelList{}, errors.New("无法读取模型列表，请检查地址或手动填写模型 ID")
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return ModelList{}, &providerStatusError{res.StatusCode}
	}
	var body struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
		Models []struct {
			Name      string   `json:"name"`
			Supported []string `json:"supportedGenerationMethods"`
		} `json:"models"`
		HasMore       bool   `json:"has_more"`
		NextPageToken string `json:"nextPageToken"`
	}
	if json.NewDecoder(io.LimitReader(res.Body, 2<<20)).Decode(&body) != nil {
		return ModelList{}, errors.New("invalid model list")
	}
	result := ModelList{Models: []string{}, More: body.HasMore || body.NextPageToken != ""}
	seen := map[string]bool{}
	add := func(id string) {
		if id != "" && !seen[id] && len(result.Models) < 500 {
			seen[id] = true
			result.Models = append(result.Models, id)
		}
	}
	for _, item := range body.Data {
		add(item.ID)
	}
	for _, item := range body.Models {
		if protocolOf(bot) == "gemini" {
			supported := false
			for _, method := range item.Supported {
				supported = supported || method == "generateContent"
			}
			if !supported {
				continue
			}
		}
		add(strings.TrimPrefix(item.Name, "models/"))
	}
	sort.Strings(result.Models)
	if len(result.Models) == 0 {
		return result, fmt.Errorf("服务未提供可用模型列表，请手动填写模型 ID")
	}
	return result, nil
}
