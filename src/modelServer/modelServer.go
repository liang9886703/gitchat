package modelServer

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	openai "github.com/sashabaranov/go-openai"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

type ChatCompletionMessage = Message

type RecvStreamContext struct {
	Ctx    *context.Context
	Cancel *context.CancelFunc
	Ch     *chan StreamMessage
}

func (c *RecvStreamContext) StopRecvStream() {
	if c != nil && c.Cancel != nil {
		(*c.Cancel)()
	}
}

type Session struct {
	Question        string
	SessionId       string
	CurChatbot      *Chatbot
	DefaultTemplate *ChatTemplate
	CurrentReq      *openai.ChatCompletionRequest
	IsFirst         bool
	mu              sync.Mutex
}
type ChatTemplate struct {
	Sysprompt, Userprompt string
	Tools                 []string
	Responseformat        string
}
type Chatbot struct {
	Cfg            *openai.ClientConfig
	IsStream       bool
	MaxTokens      int
	Model, BaseURL string
}
type SendStreamContextType int

const (
	SendStreamContextTypeInit SendStreamContextType = iota
	SendStreamContextTypeError
	SendStreamContextTypeStream
	SendStreamContextTypeEnd
)

type StreamMessage struct {
	Session                    *Session
	Message, Question, FullMsg string
	Type                       SendStreamContextType
}
type ModelServerCallback interface {
	Start() error
	FirstChar(string) error
	NextChar(string) error
	End() error
	GetError(error) error
}

func GetNewUUID() string {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		panic("cryptographic random source unavailable")
	}
	bytes[6] = (bytes[6] & 0x0f) | 0x40
	bytes[8] = (bytes[8] & 0x3f) | 0x80
	value := hex.EncodeToString(bytes)
	return value[:8] + "-" + value[8:12] + "-" + value[12:16] + "-" + value[16:20] + "-" + value[20:]
}
func InitChatbot(chatbot *Chatbot) error {
	if chatbot == nil || chatbot.Cfg == nil {
		return errors.New("chatbot configuration is missing")
	}
	if chatbot.Cfg.HTTPClient == nil {
		chatbot.Cfg.HTTPClient = &http.Client{Timeout: 90 * time.Second}
	}
	return nil
}
func CreateSession(chatbot *Chatbot, template *ChatTemplate) *Session {
	if template == nil {
		template = &ChatTemplate{}
	}
	copied := *template
	request := &openai.ChatCompletionRequest{Stream: true}
	if chatbot != nil {
		request.Model = chatbot.Model
		request.MaxTokens = chatbot.MaxTokens
	}
	if copied.Sysprompt != "" {
		request.Messages = append(request.Messages, openai.ChatCompletionMessage{Role: "system", Content: copied.Sysprompt})
	}
	return &Session{CurChatbot: chatbot, DefaultTemplate: &copied, CurrentReq: request, IsFirst: true}
}
func (s *Session) FirstStreamChat(message string, callback ModelServerCallback) (*RecvStreamContext, error) {
	return s.NextStreamChat(message, nil, callback)
}
func (s *Session) NextStreamChat(message string, messages []*ChatCompletionMessage, callback ModelServerCallback) (*RecvStreamContext, error) {
	if strings.TrimSpace(message) == "" {
		return nil, errors.New("message is empty")
	}
	s.mu.Lock()
	if s.CurChatbot == nil || s.CurChatbot.Cfg == nil {
		s.mu.Unlock()
		return nil, errors.New("chatbot configuration is missing")
	}
	request := openai.ChatCompletionRequest{Model: s.CurChatbot.Model, MaxTokens: s.CurChatbot.MaxTokens, Stream: true}
	if s.DefaultTemplate != nil && s.DefaultTemplate.Sysprompt != "" {
		request.Messages = append(request.Messages, openai.ChatCompletionMessage{Role: "system", Content: s.DefaultTemplate.Sysprompt})
	}
	for _, message := range messages {
		if message != nil {
			request.Messages = append(request.Messages, openai.ChatCompletionMessage{Role: message.Role, Content: message.Content})
		}
	}
	request.Messages = append(request.Messages, openai.ChatCompletionMessage{Role: "user", Content: message})
	s.Question = message
	s.CurrentReq = &request
	s.mu.Unlock()
	return s.start(request, message, callback), nil
}
func (s *Session) StartStreamChat(callback ModelServerCallback) (*RecvStreamContext, error) {
	s.mu.Lock()
	if s.CurrentReq == nil || s.CurChatbot == nil || s.CurChatbot.Cfg == nil {
		s.mu.Unlock()
		return nil, errors.New("session is not initialized")
	}
	request := *s.CurrentReq
	request.Messages = append([]openai.ChatCompletionMessage(nil), request.Messages...)
	question := s.Question
	s.mu.Unlock()
	return s.start(request, question, callback), nil
}
func (s *Session) start(request openai.ChatCompletionRequest, question string, callback ModelServerCallback) *RecvStreamContext {
	ch := make(chan StreamMessage, 16)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	receive := &RecvStreamContext{Ctx: &ctx, Cancel: &cancel, Ch: &ch}
	go s.receive(receive, request, question, callback)
	return receive
}
func (s *Session) RecvStream(receive *RecvStreamContext, callback ModelServerCallback) {
	s.mu.Lock()
	request := *s.CurrentReq
	request.Messages = append([]openai.ChatCompletionMessage(nil), request.Messages...)
	question := s.Question
	s.mu.Unlock()
	s.receive(receive, request, question, callback)
}
func (s *Session) receive(receive *RecvStreamContext, request openai.ChatCompletionRequest, question string, callback ModelServerCallback) {
	defer close(*receive.Ch)
	defer (*receive.Cancel)()
	ctx := *receive.Ctx
	send := func(kind SendStreamContextType, text, full string) bool {
		select {
		case *receive.Ch <- StreamMessage{Session: s, Question: question, Message: text, FullMsg: full, Type: kind}:
			return true
		case <-ctx.Done():
			return false
		}
	}
	fail := func(err error) {
		if callback != nil {
			_ = callback.GetError(err)
		}
		send(SendStreamContextTypeError, err.Error(), "")
	}
	if callback != nil {
		if err := callback.Start(); err != nil {
			fail(err)
			return
		}
	}
	client := openai.NewClientWithConfig(*s.CurChatbot.Cfg)
	stream, err := client.CreateChatCompletionStream(ctx, request)
	if err != nil {
		fail(err)
		return
	}
	defer stream.Close()
	var answer strings.Builder
	first := true
	for {
		response, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			if callback != nil {
				if err = callback.End(); err != nil {
					fail(err)
					return
				}
			}
			send(SendStreamContextTypeEnd, "[STREAM_END]", answer.String())
			return
		}
		if err != nil {
			fail(err)
			return
		}
		if len(response.Choices) == 0 || response.Choices[0].Delta.Content == "" {
			continue
		}
		text := response.Choices[0].Delta.Content
		answer.WriteString(text)
		if callback != nil {
			if first {
				err = callback.FirstChar(text)
			} else {
				err = callback.NextChar(text)
			}
			if err != nil {
				fail(err)
				return
			}
		}
		first = false
		if !send(SendStreamContextTypeStream, text, answer.String()) {
			return
		}
	}
}
