package cards

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"gitchat/src/modelServer"
)

// 用string而不是uuid是为了增加可读性，可以在uuid后面加上中文拼音
type AppId struct {
	MainId string // 类似gpt的项目概念，不同appid不同内置提示词
}
type SessionId struct { //注意这个sessionid和网络传输的字段完全不同
	_      struct{}
	MainId string // 一个appid下的一次聊天，对应一个无环图
}

//Version和cardId的区别是，前者标记具体的卡片内容，后者标识这个卡片的位置
//一个典型的场景是，这张卡片的回复终止了，或者觉得回答的不好要覆盖这个卡片的内容，此时这个卡片位置没有改变但是内容变了

func newSessionId() (SessionId, error) {
	// node, err := getSnowflakeNode()
	// if err != nil {
	// 	return SessionId{}, err
	// }
	return SessionId{MainId: modelServer.GetNewUUID()}, nil
}

func (s *SessionId) IsEmpty() bool {
	return strings.TrimSpace(s.MainId) == ""
}
func (s *AppId) IsEmpty() bool {
	return strings.TrimSpace(s.MainId) == ""
}
func (s *Version) IsEmpty() bool {
	return strings.TrimSpace(s.MainId) == ""
}
func (s *Version) String() string {
	return s.MainId
}
func (s *SessionId) String() string {
	return s.MainId
}
func (s *AppId) String() string {
	return s.MainId
}

func VersionFromString(str string) Version {
	return Version{MainId: strings.TrimSpace(str)}
}
func SessionIdFromString(str string) SessionId {
	return SessionId{MainId: strings.TrimSpace(str)}
}
func AppIdFromString(str string) AppId {
	return AppId{MainId: strings.TrimSpace(str)}
}

// 通过已有的卡片节点创建一个他的子节点
func (s *session) createDeepenCard(old *cardNode, index int64, isEmpty bool) *cardNode {
	newcard := newCardNode(index, isEmpty, old.ModelSession)
	newcard.statusChange(CardStatusCreating)
	// 更新图结构
	deepenGraphInfo(&old.Graph, &newcard.Graph, newcard.Identity)
	// 更新事件
	newcard.CardEvt.init(OpeDeepen, old.Identity, []cardIdentity{old.Identity}, newcard.Identity)
	// 更新聊天记录
	newcard.initChatsFromOther(old)
	return newcard
}

type modelServerCallback struct {
	card *cardNode
}

func (callback *modelServerCallback) Start() error {
	callback.card.statusChange(CardStatusRequesting)

	fmt.Print("--------------------------------[callback]start--------------------------------\n")
	return nil
}

func (callback *modelServerCallback) FirstChar(message string) error {
	fmt.Println("[callback]FirstChar")

	callback.card.statusChange(CardStatusToken)

	callback.card.updateMsg(message)
	// card.chats[len(card.chats)-1].reciveAnswer([]*ChatCompletionMessage{
	// 	{
	// 		Role:    "assistant",
	// 		Content: message,
	// 	},
	// })
	// card.chats[len(card.chats)-1].Identity = card.Identity
	fmt.Print(message)
	return nil
}

// 接口的参数名字不同，还能继承接口吗?
func (callback *modelServerCallback) NextChar(message string) error {
	callback.card.updateMsg(message)

	callback.card.statusChange(CardStatusToken)

	fmt.Printf("%s", message)
	return nil
}

func (callback *modelServerCallback) End() error {
	fmt.Println("")
	fmt.Println("[callback]End")
	callback.card.updateMsgEnd()
	callback.card.statusChange(CardStatusFinish)

	fmt.Print("--------------------------------[callback]end--------------------------------\n")
	//todo这里是临时方案，未来需要网络接收到压缩上下文并更新
	callback.card.compressContext.toChatCompletionMessages()
	return nil
}

func (callback *modelServerCallback) GetError(err error) error {
	fmt.Println("[callback]GetError")
	callback.card.statusChange(CardStatusError)

	return nil
}

func getModelServerCallback(card *cardNode) *modelServerCallback {
	return &modelServerCallback{
		card: card,
	}
}

type cardsGrouptatus int //卡片的生命周期

const (
	CardsGroupInit cardsGrouptatus = iota
	CardsGroupRequesting
	CardsGroupArchiving //先把所有记录得到一张总结卡片，再持久化
	CardsGroupArchived
)

type session struct { //标识和用户建立的一次对话
	cards []*cardNode //第0个卡片一定是一张空白初始化卡片，用户的卡片从第一张开始
	// todo：多个协程可能对同一个位置进行操作，因此需要加锁
	lastIndex   int64                 //最后一个卡片的位置
	lastVersion Version               //上一次对话的版本，用户返回错误的版本则回复所有版本，并刷新用户界面，重新发送数据，这个字段保留
	cardsMap    map[Version]*cardNode //空白卡片得有版本，不然无法定位
	app         *application

	isSummarize bool
	isExist     bool
	status      cardsGrouptatus

	sessionId        SessionId
	appId            AppId
	createTime       time.Time
	lastcardIdentity cardIdentity
	lastcardTime     time.Time
} //定义

type application struct {
	// 通过session里的lastcardTime来判断哪个session是活跃的，定时任务持久化并清理不活跃的session
	sessions map[SessionId]*session

	chatbot      *modelServer.Chatbot //管理对话传输配置
	chattemplate modelServer.ChatTemplate

	appId   AppId
	appName string

	createTime       time.Time
	lastcardIdentity cardIdentity
	lastcardTime     time.Time
} //定义

func (app *application) createNewSession() (SessionId, *session, Version) {
	var (
		sessionId SessionId = SessionId{MainId: modelServer.GetNewUUID()}
		version   Version   = Version{MainId: modelServer.GetNewUUID()}
	)
	//fmt.Println(app.chatbot)
	//fmt.Println("check chatbot")
	//每个app都从一张空白卡片开始
	card := newCardNode(0, true, modelServer.CreateSession(app.chatbot, &app.chattemplate))
	app.sessions[sessionId] = &session{
		cards:       []*cardNode{card},
		lastIndex:   0,
		cardsMap:    map[Version]*cardNode{version: card},
		lastVersion: version,

		isSummarize: false,
		isExist:     true,
		status:      CardsGroupInit,

		sessionId:        sessionId,
		appId:            app.appId,
		createTime:       time.Now(),
		lastcardIdentity: card.Identity,
		lastcardTime:     time.Now(),
	}
	app.lastcardIdentity = card.Identity
	app.lastcardTime = time.Now()
	return sessionId, app.sessions[sessionId], version
}

func (s *session) updateSessionWithNewCard(newcard *cardNode) {
	s.cards = append(s.cards, newcard)
	s.lastVersion = newcard.Identity.Version
	s.cardsMap[newcard.Identity.Version] = newcard

	s.lastcardIdentity = newcard.Identity
	s.lastcardTime = newcard.CardEvt.EventTime
}

func (s *session) firstStreamChatOnce(versionId Version, msg string) (modelServer.RecvStreamContext, Version, error) {
	var (
		card *cardNode
		ok   bool
	)

	if card, ok = s.cardsMap[versionId]; !ok {
		return modelServer.RecvStreamContext{}, versionId, errors.New("versionId no exist")
	}

	// todo: 新建一个卡片，这是需要上锁的，避免多个协程在同一个位置新建卡片
	s.lastIndex++

	newcard := s.createDeepenCard(card, s.lastIndex, false)
	s.updateSessionWithNewCard(newcard)

	newcard.statusChange(CardStatusCreated)

	//发消息
	content, err := newcard.ModelSession.FirstStreamChat(msg, getModelServerCallback(newcard))
	if err != nil {
		return modelServer.RecvStreamContext{}, versionId, err
	}

	return *content, newcard.Identity.Version, nil
}

func (s *session) nextStreamChatOnce(versionId Version, msg string, ope operation) (modelServer.RecvStreamContext, Version, error) {

	var (
		card    *cardNode
		ok      bool
		newcard *cardNode
		ctx     []*ChatCompletionMessage
	)

	if card, ok = s.cardsMap[versionId]; !ok {
		return modelServer.RecvStreamContext{}, versionId, errors.New("versionId no exist")
	}

	// todo: 新建一个卡片，这是需要上锁的，避免多个协程在同一个位置新建卡片
	s.lastIndex++
	switch ope {
	case OpeDeepen:
		newcard = s.createDeepenCard(card, s.lastIndex, false)
	case OpeBranch: //todo
		s.lastIndex++
	case OpeMerge:
		s.lastIndex++
	default:
		return modelServer.RecvStreamContext{}, versionId, errors.New("invalid operation")
	}

	s.updateSessionWithNewCard(newcard)

	newcard.statusChange(CardStatusCreated)

	if card.isCompressContext {
		compressedContext, err := GetCompressContext(card)
		if err != nil {
			// 失败的情况是模型侧迟迟没有返回压缩的上下文，此时请求失败
			return modelServer.RecvStreamContext{}, versionId, err
		}
		ctx = compressedContext.toChatCompletionMessages()
		//发消息
	} else {
		ctx = card.chats.toChatCompletionMessages()
	}
	for _, msg := range ctx {
		fmt.Println(msg.Role, msg.Content)
	}
	content, err := newcard.ModelSession.NextStreamChat(msg,
		ctx,
		getModelServerCallback(newcard),
	)
	if err != nil {
		return modelServer.RecvStreamContext{}, versionId, err
	}
	//todo 同意一下错误时的返回处理

	return *content, newcard.Identity.Version, nil
}
