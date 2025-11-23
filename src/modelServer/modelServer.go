package modelServer

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/google/uuid"
	openai "github.com/sashabaranov/go-openai"
)

type ChatCompletionMessage struct {
	Role    string `json:"role"`
	Content string `json:"content,omitempty"`
}

// 提供给外部使用管理已经进行的对话，主要用于取消对话
type RecvStreamContext struct {
	Ctx    *context.Context
	Cancel *context.CancelFunc
	Ch     *chan StreamMessage
}

// 标识和模型服务建立的一次对话
type Session struct {
	Question        string
	SessionId       string
	CurChatbot      *Chatbot
	DefaultTemplate *ChatTemplate
	CurrentReq      *openai.ChatCompletionRequest //用户的问题会在这里覆盖旧的问题
	IsFirst         bool                          //是否是第一次对话
	ctx             *RecvStreamContext
}

type ChatTemplate struct {
	Sysprompt      string
	Userprompt     string //openai没有这个东西，为我的服务后续扩展做准备
	Tools          []string
	Responseformat string
}

// 一个chatbot对应全局会话管理
type Chatbot struct {
	//Sessions  map[uuid.UUID]Session
	Cfg       *openai.ClientConfig
	IsStream  bool
	MaxTokens int
	Model     string
	BaseURL   string
	client    *openai.Client
}

type SendStreamContextType int

const (
	SendStreamContextTypeInit   SendStreamContextType = iota
	SendStreamContextTypeError                        // 包括：创建错误、网络错误、主动停止
	SendStreamContextTypeStream                       // 流式返回token
	SendStreamContextTypeEnd                          //流式结束
)

type StreamMessage struct {
	Session  *Session
	Message  string
	Question string
	FullMsg  string
	Type     SendStreamContextType
}

// todo；项目中太多的无效命名，全部用继承来代替
type ModelServerCallback interface {
	Start() error
	FirstChar(message string) error
	NextChar(message string) error
	End() error
	GetError(err error) error
}

// todo 上层维护一个cancel全局数组，用于中止会话
func (ctx *RecvStreamContext) StopRecvStream() {
	(*ctx.Cancel)()
}

func GetNewUUID() string {
	return uuid.New().String()
}

func InitChatbot(chatbot *Chatbot) error {
	chatbot.Cfg.HTTPClient = &http.Client{
		Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
			//logger.Println(">>>", req.Method, req.URL.String())
			return http.DefaultTransport.RoundTrip(req)
		}),
	}
	return nil
}

var defaultMsg []openai.ChatCompletionMessage

func CreateSession(chatbot *Chatbot, template *ChatTemplate) *Session {
	defaultMsg = []openai.ChatCompletionMessage{
		{
			Role:    openai.ChatMessageRoleSystem,
			Content: template.Sysprompt,
		},
		{
			Role:    openai.ChatMessageRoleUser,
			Content: template.Userprompt,
		},
	}
	return &Session{
		CurChatbot:      chatbot,
		DefaultTemplate: template,
		CurrentReq: &openai.ChatCompletionRequest{
			Model:     chatbot.Model,
			MaxTokens: chatbot.MaxTokens,
			Messages:  defaultMsg,
			Stream:    chatbot.IsStream,
		},
		IsFirst: true,
	}
}

// 第一次对话是创建对话结构，将用户对话填入发送的消息字段
func (session *Session) FirstStreamChat(message string, callback ModelServerCallback) (*RecvStreamContext, error) {
	var err error

	session.Question = message
	//todo：发送到远端对话存储服务器
	for i := range session.CurrentReq.Messages {
		if session.CurrentReq.Messages[i].Role == openai.ChatMessageRoleUser {
			session.CurrentReq.Messages[i].Content = message
			//fmt.Println("message ready", session.Currentreq.Messages[i])
			//return nil, errors.New("message content error")
		}

	}
	session.CurChatbot.client = openai.NewClientWithConfig(*session.CurChatbot.Cfg)
	if session.CurChatbot.client == nil {
		return nil, errors.New("create client failed")
	}

	session.ctx, err = session.StartStreamChat(callback)
	return session.ctx, err
}

// 第n次对话是从上下文模块里拿到所有对话，遍历生成消息字段
func (session *Session) NextStreamChat(message string, messages []*ChatCompletionMessage, callback ModelServerCallback) (*RecvStreamContext, error) {
	if session.CurChatbot.client == nil {
		//todo:日志，非第一次对话，但client为空
		session.CurChatbot.client = openai.NewClientWithConfig(*session.CurChatbot.Cfg)
		if session.CurChatbot.client == nil {
			return nil, errors.New("create client failed")
		}
	}

	session.CurrentReq.Messages = make([]openai.ChatCompletionMessage, 0, len(messages)+len(defaultMsg)+1)
	session.CurrentReq.Messages = append(session.CurrentReq.Messages, defaultMsg...)

	for _, msg := range messages {
		session.CurrentReq.Messages = append(session.CurrentReq.Messages, openai.ChatCompletionMessage{
			Role:    msg.Role,
			Content: msg.Content,
		})
	}
	session.CurrentReq.Messages = append(session.CurrentReq.Messages, openai.ChatCompletionMessage{
		Role:    openai.ChatMessageRoleUser,
		Content: message,
	})

	return session.StartStreamChat(callback)
}

// 构件消息管理对象，另起协程发起网络请求
func (session *Session) StartStreamChat(callback ModelServerCallback) (*RecvStreamContext, error) {

	//resp, err := client.CreateChatCompletion(context.Background(), req)
	// fmt.Println(resp.Choices[0].Message.Content)
	// req.Messages = append(req.Messages, resp.Choices[0].Message)
	// return resp.Choices[0].Message.Content, err

	// 创建 channel 用于流式输出
	ch := make(chan StreamMessage, 10) // todo：读配置，设置缓冲区大小
	//defer close(ch)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	//defer cancel()

	//todo：这里应该用create来创建，在创建中打出日志
	recvStreamContext := RecvStreamContext{
		Ctx:    &ctx,
		Cancel: &cancel,
		Ch:     &ch,
	}

	go session.RecvStream(&recvStreamContext, callback)

	return &recvStreamContext, nil
}

// todo：接入日志级别
// 启动 goroutine 处理流式响应
func (session *Session) RecvStream(recvStreamContext *RecvStreamContext, callback ModelServerCallback) {
	stream, err := session.CurChatbot.client.CreateChatCompletionStream(*recvStreamContext.Ctx, *session.CurrentReq)
	if err != nil {
		callback.GetError(err)

		*recvStreamContext.Ch <- StreamMessage{
			Session:  session,
			Message:  "[CREATE_ERROR]" + err.Error(),
			Question: session.Question,
			FullMsg:  "",
			Type:     SendStreamContextTypeError,
		}
		return
	}
	defer stream.Close()
	defer (*recvStreamContext.Cancel)()

	var contents string
	sendStreamContext := StreamMessage{
		Session:  session,
		Message:  "[STREAM_END]",
		Question: session.Question,
		FullMsg:  contents,
		Type:     SendStreamContextTypeEnd,
	}

	callback.Start()
	//stream.Header().Get("request_id")
	// fmt.Println(stream.Header())
	for {

		response, err := stream.Recv() //这里会让出线程，从而给其他协程调用cancel的机会
		if errors.Is(err, io.EOF) {
			sendStreamContext.Message = "[STREAM_END]"
			sendStreamContext.Type = SendStreamContextTypeEnd
			*recvStreamContext.Ch <- sendStreamContext
			//3. todo 记录收到的所有token，发送给存储服务器
			callback.End()
			return
		}

		if err != nil {
			sendStreamContext.Message = "[STREAM_ERROR]" + err.Error()
			sendStreamContext.Type = SendStreamContextTypeError
			*recvStreamContext.Ch <- sendStreamContext
			callback.GetError(err)
			return
		}
		if len(response.Choices) != 0 {
			//2. 每收到一个token，就发送给客户端
			content := func() string {
				defer func() {
					if r := recover(); r != nil {
						fmt.Printf("Error extracting content from response.Choices[0].Delta.Content: %v", r)
					}
				}()
				if response.Choices[0].Delta.Content != "" {
					return response.Choices[0].Delta.Content
				}
				return ""
			}()

			if content != "" {
				sendStreamContext.Message = content
				sendStreamContext.Type = SendStreamContextTypeStream
				*recvStreamContext.Ch <- sendStreamContext
				contents += content
			}

			if session.IsFirst {
				//1. todo：接受并存储信息头，修改后续请求路径

				//fmt.Println("--------------------------------")
				//fmt.Println("response:", response)
				//fmt.Println("--------------------------------")
				session.SessionId = response.ID
				session.IsFirst = false
				callback.FirstChar(response.Choices[0].Delta.Content)
			} else {
				callback.NextChar(response.Choices[0].Delta.Content)
			}
		}
	}
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
