package main

import (
	"fmt"
	"gitchat/src/cards"
	"gitchat/src/config"
	"gitchat/src/modelServer"
	"os"
	"strings"
	"testing"
)

var isFirst bool = true

// 测试用例
var testCases = []struct {
	name    string
	message string
	wantErr bool
}{
	{
		name:    "1+1=",
		message: "1+1=",
		wantErr: false,
	},
	{
		name:    "再+35？",
		message: "再+35？",
		wantErr: false,
	},
	{
		name:    "再+31？",
		message: "再+31？",
		wantErr: false,
	},
}

// TestSendMessage 测试 SendMessage 函数
func TestSendMessage(t *testing.T) {
	//检查环境变量
	if os.Getenv("OPENAI_API_KEY_0") == "" {
		t.Skip("跳过测试：需要设置 OPENAI_API_KEY 环境变量")
	}

	configs, err := config.GetConfig()
	if err != nil {
		fmt.Printf("Failed to get config: %v\n", err)
	}
	cardsServer, err := cards.CreateCardServer(configs)
	if err != nil {
		t.Errorf("Failed to create cards server: %v\n", err)
	}

	//用默认的应用（无内置提示词）创建一次对话
	sessionId, version, err := cardsServer.CreateNewSessionByApp(0)
	if err != nil {
		t.Errorf("Failed to create session: %v", err)
	}
	fmt.Println("start")

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			var (
				err error
			)
			call := RecvMessage(cardsServer, sessionId, version, tc.message)

			version, err = call()
			if !tc.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}

		})
	}
}

// SendMessage 保持向后兼容，内部使用 StreamMessage
func RecvMessage(cardsServer cards.CardServer, sessionId cards.SessionId, version cards.Version, message string) func() (cards.Version, error) {

	return func() (cards.Version, error) {
		var fullResponse strings.Builder
		var res modelServer.RecvStreamContext
		var err error
		if isFirst {
			fmt.Println("first")
			res, version, err = cardsServer.FirstStreamChatOnce(sessionId, version, message)
			//_, version, err = cardsServer.FirstStreamChatOnce(sessionId, version, message)
		} else {
			fmt.Println("second")
			res, version, err = cardsServer.NextStreamChatOnce(sessionId, version, message)
			//_, version, err = cardsServer.NextStreamChatOnce(sessionId, version, message)
		}
		if err != nil {
			fmt.Printf("Failed to add chat to session: %v\n", err)
			return version, err
		}
		isFirst = false

		// fmt.Println("--------------start----------------")
		for content := range *res.Ch {
			switch content.Type {
			case modelServer.SendStreamContextTypeStream:
				fullResponse.WriteString(content.Message)
				//fmt.Printf("%s", content.Message)
			case modelServer.SendStreamContextTypeEnd:
				//fmt.Printf("%s\n", content.Message)
				return version, nil
			case modelServer.SendStreamContextTypeError:
				//fmt.Printf("%s\n", content.Message)
				return version, fmt.Errorf("receive stream error:%s", content.Message)
			default:
				//fmt.Printf("%s\n", content.Message)
				return version, fmt.Errorf("rev is unkown type")
			}
		}

		// fmt.Println("--------------end----------------")
		return version, fmt.Errorf("cant't get stream message")
	}
}
