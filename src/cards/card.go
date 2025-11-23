package cards

import (
	"gitchat/src/modelServer"
	"slices"
	"strings"
	"time"
)

type Version struct {
	MainId string // 版本id，雪花生成，标识一张唯一的卡片，客户端生成？防止ABA问题
}
type cardId int64 // 标识一张卡片,确定图中唯一一个位置，卡片数组的下标

type cardIdentity struct {
	_       struct{}
	cardId  // 卡片在会话中的位置，从0开始，用于在应用内标识一张卡片，可能也需要返回给客户端，客户端发起时携带
	Version // 版本id，用于校验，防止用户合并卡片的时候用错误的卡片索引
	/*SessionId用雪花，版本用递增的原因是应用是随用随删的，没有递增关系，如果要导入旧的对话数据，不需要考虑重排id
	  分布式友好：多个实例并发创建节点不冲突，可水平扩展，避免自增主键热点。
	  更安全：不暴露逐号递增（避免"穷举 ID"），对外作为资源标识更合适。
	   converstaionId由服务器提供，而VersionId由客户端提供，这里只做校验
	   VersionId以服务器为准，需要上锁访问，防止多个浏览器页面请求，如果请求旧的id，返回已有数据和新id的数据让浏览器更新*/
}

func getCardFromId[T interface {
	int64 | cardId | Version | cardIdentity
}](s *session, id T) *cardNode {
	switch v := any(id).(type) {
	case int64:
		return s.cards[v]
	case cardId:
		return s.cards[int64(v)]
	case Version:
		return s.cardsMap[v]
	case cardIdentity:
		return s.cardsMap[v.Version]
	default:
		return nil
	}
}

// Equal 比较两个 cardIdentity 是否相等
func (c cardIdentity) Equal(other cardIdentity) bool {
	return c == other
}

func newcardIdentity(index int64) cardIdentity {
	// 获取 snowflake node（可能返回错误）
	// node, err := getSnowflakeNode()
	// if err != nil {
	// 	return cardIdentity{}, err
	// }

	// versionId := Version{MainId: node.Generate().Int64()}

	return cardIdentity{
		cardId:  cardId(index),
		Version: Version{MainId: modelServer.GetNewUUID()},
	}
}

type graphInfo struct {
	_               struct{}
	ParentsVersions *[]cardIdentity // 父节点Version的数组
	ChildVersions   *[]cardIdentity // 子节点Version的数组指针，创建子节点的时候继承这个指针，这样只需要修改一个内存就能将新增节点的信息同步给所有节点
	SiblingVersions *[]cardIdentity // 兄弟节点Version的数组指针
	identity        cardIdentity
	level           int64 // 层级，从0开始
}

func (this *graphInfo) getCommonAncestor(other *graphInfo) cardId {
	return cardId(0)
}

func newgraphInfo() graphInfo {
	return graphInfo{
		ParentsVersions: &[]cardIdentity{},
		ChildVersions:   &[]cardIdentity{},
		SiblingVersions: &[]cardIdentity{},
		level:           0,
	}
}

// todo
// 上一层拆分成执行两次这个函数，第一次建空卡片，第二次建合并卡片
func (graph *graphInfo) mergeGraphInfo(other cardIdentity, index cardIdentity, ope operation) (graphInfo, error) {
	return graphInfo{}, nil
}

// NewgraphInfo 根据操作类型和已有的 graphInfo 构建新的 graphInfo
// todo：对图的修改必须互斥的，通过对图上全局锁来实现，更细粒度的锁可以通过锁住单个节点来实现但这比较麻烦，而且对一个图发起多个操作的场景都是一个用户发起的，同时提了多个问题，所以整个图串行回复不影响体验
func deepenGraphInfo(old *graphInfo, new *graphInfo, other cardIdentity) {
	// 感觉区分branch和deepen没有必要，对于发散的逻辑有两种处理方式，一个是传递用户当前的卡片，创建他的伙伴卡片，另一种是传递用户当前卡片的父卡片，创建他的子卡片。我选择了后者，因为传过来用户当前所在的卡片并没有价值，实现还要增加找父卡片的函数
	//可能出现用户在一个节点下同时创建了两个节点，一个深入一个发散，而发散的先到，发现没有兄弟
	if old.ChildVersions == nil {
		old.ChildVersions = &[]cardIdentity{}
	}
	*old.ChildVersions = append(*old.ChildVersions, other)

	new.ParentsVersions = &[]cardIdentity{old.identity}
	new.ChildVersions = &[]cardIdentity{}
	new.SiblingVersions = old.ChildVersions
	new.identity = other
	new.level = old.level + 1
}

type operation int

const (
	OpeInit operation = iota
	OpeDeepen
	OpeBranch
	OpeMerge
)

type cardEvent struct {
	Operation      operation      `json:"operation"`
	CurrentCard    cardIdentity   `json:"current_card"`     // 从哪个卡片发起的这个事件
	ParentscardIds []cardIdentity `json:"parents_card_ids"` //发散和深入这个没有意义，用于合并的
	NewCard        cardIdentity   `json:"new_card"`

	EventId   string    `json:"event_id"`
	EventTime time.Time `json:"event_time"`
}

func (e *cardEvent) init(ope operation, currentCard cardIdentity, parentscardIds []cardIdentity, newCard cardIdentity) {
	e.Operation = ope
	e.CurrentCard = currentCard
	e.ParentscardIds = parentscardIds
	e.NewCard = newCard
	e.EventId = modelServer.GetNewUUID()
	e.EventTime = time.Now()
}

type cardStatus int //卡片的生命周期

const (
	CardStatusInit cardStatus = iota
	CardStatusCreating
	CardStatusCreated

	CardStatusRequesting //请求模型中
	CardStatusToken      //流式返回token中
	CardStatusPaused     //用户终止对话。不能恢复，不能删除这个卡片？
	CardStatusError      //todo：error类型
	CardStatusFinish

	CardStatusArchived
	CardStatusArchiving
)

type cardNode struct {
	_ struct{}

	builder           *strings.Builder //暂存接收到的模型回答
	chats             rawContexts      // 对于一张卡片来说，这个数组的大小是确定的，一张卡片一定是线性关系，下标从前往后对应一次对话的历史
	compressContext   compressContexts // 压缩的上下文
	isCompressContext bool             // 是否压缩了上下文，也许用enum

	CardEvt      cardEvent
	ModelSession *modelServer.Session // 对于平行的卡片来说，各个卡片之间的网络请求是独立的，但对于串行的卡片来说，网络请求是共享的
	State        cardStatus

	Identity cardIdentity
	Graph    graphInfo

	IsEmpty bool //第一次时对话基于一个空卡片开始，合并对话后也先建一个空卡片，空卡片下面才是真正的卡片，目的是为了让合并对话的卡片可以发散
}

// CardNode 的构造函数
func newCardNode(index int64, isEmpty bool, modelSession *modelServer.Session) *cardNode {
	return &cardNode{
		builder:         &strings.Builder{},
		chats:           rawContexts{&oneChat{}},
		compressContext: compressContexts([]*oneChat{}),

		ModelSession: modelSession,

		Identity: newcardIdentity(index),
		Graph:    newgraphInfo(),

		IsEmpty: isEmpty,
	}
}

func (card *cardNode) initChatsFromOther(other *cardNode) {
	if other == nil || other.chats == nil {
		//todo：日志
		return
	}

	chatNums := len(other.chats)
	card.chats = make(rawContexts, chatNums, chatNums+1)

	//拷贝所有轮次的历史对话，第一张卡片里的对话内容是系统提示词
	// 深拷贝：创建新的切片和内容,原因参考card.md中卡片中的“历史对话消息管理”部分
	for i, chat := range other.chats {
		if chat == nil {
			continue
		}

		// 用一块新的内存来存放管理消息的数组指针
		card.chats[i] = chat
	}
}

func (card *cardNode) updateMsg(msg string) {
	if card == nil || card.chats == nil || card.chats[len(card.chats)-1] == nil {
		return
	}
	if card.builder == nil {
		card.builder = &strings.Builder{}
	}
	card.builder.WriteString(msg)
}

func (card *cardNode) updateMsgEnd() {
	if card == nil || card.chats == nil || card.chats[len(card.chats)-1] == nil {
		return
	}
	card.chats = append(card.chats, &oneChat{
		Identity: card.Identity,
		contents: []*ChatCompletionMessage{
			{
				Role:    "assistant",
				Content: card.builder.String(),
			},
		},
	})
	card.builder.Reset()
}

func (card *cardNode) statusChange(status cardStatus) {
	defer func() {
		if err := recover(); err != nil {
			card.State = CardStatusError
		}
	}()
	// 卡片状态的状态机合法转变表
	validTransitions := map[cardStatus][]cardStatus{
		CardStatusInit:       {CardStatusCreating, CardStatusError},
		CardStatusCreating:   {CardStatusCreated, CardStatusError},
		CardStatusCreated:    {CardStatusRequesting, CardStatusError},
		CardStatusRequesting: {CardStatusToken, CardStatusPaused, CardStatusFinish, CardStatusError},
		CardStatusToken:      {CardStatusToken, CardStatusPaused, CardStatusFinish, CardStatusError},
		CardStatusFinish:     {CardStatusArchiving, CardStatusError},
		CardStatusPaused:     {CardStatusRequesting, CardStatusToken, CardStatusFinish, CardStatusError},
		CardStatusArchiving:  {CardStatusArchived, CardStatusError},
	}

	if status == CardStatusInit {
		panic("CardStatusInit is not a valid status")
	}
	if !slices.Contains(validTransitions[card.State], status) {
		panic("statusChange: invalid status,from " + card.State.String() + " to " + status.String())
	}
	card.State = status
}

func (status cardStatus) String() string {
	switch status {
	case CardStatusInit:
		return "CardStatusInit"
	case CardStatusCreating:
		return "CardStatusCreating"
	case CardStatusCreated:
		return "CardStatusCreated"
	case CardStatusRequesting:
		return "CardStatusRequesting"
	case CardStatusToken:
		return "CardStatusToken"
	case CardStatusPaused:
		return "CardStatusPaused"
	case CardStatusError:
		return "CardStatusError"
	case CardStatusFinish:
		return "CardStatusFinish"
	case CardStatusArchived:
		return "CardStatusArchived"
	case CardStatusArchiving:
		return "CardStatusArchiving"
	default:
		return "unknown status"
	}
}
