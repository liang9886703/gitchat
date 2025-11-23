package cards

import (
	"gitchat/src/modelServer"
	"sync"
)

type ChatCompletionMessage = modelServer.ChatCompletionMessage

// 管理一次用户和模型的对话历史上下文
type oneChat struct {
	contents []*ChatCompletionMessage //"role": "user", "content": "1+1=?",在转为消息发送时，role和content交换位置不影响的
	Identity cardIdentity             // 标志这个对话对应的卡片
	//引用
}

// 管理所有历史对话
type rawContexts []*oneChat
type compressContexts []*oneChat

func createOneChat(answer []*ChatCompletionMessage, identity cardIdentity) *oneChat {
	return &oneChat{
		contents: answer,
		Identity: identity,
	}
}

// 用于增加角色的回答
func (onechat *oneChat) reciveAnswer(answer []*ChatCompletionMessage) {
	onechat.contents = append(onechat.contents, answer...)
}

func (ctx *rawContexts) toChatCompletionMessages() []*ChatCompletionMessage {
	if ctx == nil {
		return nil
	}

	// for _, onechat := range *ctx {
	// 	if onechat != nil {
	// 		for _, msg := range onechat.contents {
	// 			fmt.Println(msg.Role, msg.Content)
	// 		}
	// 	}
	// }

	var totalCap int = 0
	for _, onechat := range *ctx {
		if onechat != nil {
			totalCap += len(onechat.contents)
		}
	}

	result := make([]*ChatCompletionMessage, 0, totalCap)

	for _, onechat := range *ctx {
		if onechat == nil {
			continue
		}
		result = append(result, onechat.contents...)
	}
	return result
}

func (ctx *compressContexts) toChatCompletionMessages() []*ChatCompletionMessage {
	if ctx == nil {
		return nil
	}
	totalCap := 0
	for _, onechat := range *ctx {
		if onechat != nil {
			totalCap += len(onechat.contents)
		}
	}

	var result []*ChatCompletionMessage
	for _, onechat := range *ctx {
		if onechat == nil {
			continue
		}
		result = append(result, onechat.contents...)
	}
	return result
}

// 压缩是必要的，一定等前面压缩完成了，才会进行下一轮对话
func GetCompressContext(card *cardNode) (*compressContexts, error) {
	//todo：上锁查看压缩的上下文结果，模型侧迟迟没有返回压缩的上下文，则返回错误
	contexts := make([]*oneChat, 0, len(card.chats))
	contexts = append(contexts, card.chats...)

	// &((compressContexts)contexts) 这样不行，因为结构体之间的类型转换会触发拷贝行为，拷贝的临时变量不能对其取值
	return (*compressContexts)(&contexts), nil
}

// todo：这个事再说
// 一个全局变量数组，用于存储请求句柄
var global any
var globalMutex sync.Mutex
var status []bool

// 这是一个后台定时任务
func reciveCompressContext(time int) {
	//查看status，数组较大且大多数都是空，则一次性更换数组
	//上锁遍历数组，查看是否完成了请求句柄，填入内容，修改状态为完成
	//继续下一轮定时器，time为时间间隔
}
