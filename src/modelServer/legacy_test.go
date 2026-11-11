package modelServer

import (
	"fmt"
	openai "github.com/sashabaranov/go-openai"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type recordingCallback struct {
	started, ended, failed int
	answer                 strings.Builder
}

func (c *recordingCallback) Start() error             { c.started++; return nil }
func (c *recordingCallback) FirstChar(s string) error { c.answer.WriteString(s); return nil }
func (c *recordingCallback) NextChar(s string) error  { c.answer.WriteString(s); return nil }
func (c *recordingCallback) End() error               { c.ended++; return nil }
func (c *recordingCallback) GetError(error) error     { c.failed++; return nil }

func TestLegacyStreamClosesOnSuccessAndError(t *testing.T) {
	for _, success := range []bool{true, false} {
		t.Run(fmt.Sprint(success), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if !success {
					w.WriteHeader(http.StatusBadGateway)
					return
				}
				w.Header().Set("Content-Type", "text/event-stream")
				fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"Hello\"}}]}\n\ndata: [DONE]\n\n")
			}))
			defer server.Close()
			cfg := openai.DefaultConfig("test")
			cfg.BaseURL = server.URL
			bot := &Chatbot{Cfg: &cfg, Model: "test", IsStream: true}
			if err := InitChatbot(bot); err != nil {
				t.Fatal(err)
			}
			session := CreateSession(bot, &ChatTemplate{Sysprompt: "help"})
			callback := &recordingCallback{}
			stream, err := session.FirstStreamChat("question", callback)
			if err != nil {
				t.Fatal(err)
			}
			terminal := false
			deadline := time.After(2 * time.Second)
			for {
				select {
				case event, ok := <-*stream.Ch:
					if !ok {
						if !terminal {
							t.Fatal("closed without terminal event")
						}
						if success && (callback.ended != 1 || callback.answer.String() != "Hello") {
							t.Fatalf("wrong callback state %+v", callback)
						}
						if !success && callback.failed != 1 {
							t.Fatalf("error callback not called %+v", callback)
						}
						return
					}
					if event.Type == SendStreamContextTypeEnd || event.Type == SendStreamContextTypeError {
						terminal = true
					}
				case <-deadline:
					t.Fatal("stream channel was not closed")
				}
			}
		})
	}
}
