package cards

import (
	"errors"
	"log" //todo 成熟的日志框架
	"os"
	"time"

	"gitchat/src/config"

	"gitchat/src/modelServer"

	"github.com/bwmarrin/snowflake"
	openai "github.com/sashabaranov/go-openai"
)

func getSnowflakeNode(cServer *CardServer) (*snowflake.Node, error) {
	return cServer.getNode()
}

type CardServer struct {
	snowflakeManager *snowflake.Node
	configs          *config.Config
	logger           *log.Logger
	sessions         map[SessionId]*session
	apps             map[AppId]*application //只通过appid找到app，不通过name查找
	chatbots         map[string]*modelServer.Chatbot
	defaultAppId     AppId
	// 用户首先获取全部的appid，然后根据appid新建一个session，每个对话通过这个sessionId来对话
	// 因此需要通过sessionId找到appid，也需要appid找到sessionid
}

func CreateCardServer(cfg *config.Config) (CardServer, error) {
	//RAII，资源获取即初始化，初始化失败资源释放
	var (
		cServer CardServer = CardServer{
			snowflakeManager: &snowflake.Node{},
			chatbots:         make(map[string]*modelServer.Chatbot),
			sessions:         make(map[SessionId]*session),
			apps:             make(map[AppId]*application),
		}
		err error
	)

	// 初始化日志
	cServer.logger = log.New(os.Stdout, "", log.LstdFlags)
	cServer.logger.SetFlags(log.LstdFlags)
	cServer.logger.SetPrefix("gitchat: ")
	cServer.logger.SetFlags(log.LstdFlags | log.Ldate | log.Ltime)

	cServer.configs = cfg

	snowflake.Epoch = time.Now().UnixNano() / 1e6
	cServer.snowflakeManager, err = snowflake.NewNode(1) // ← 直接返回值和错误
	if err != nil {
		return CardServer{}, err
	}
	// 初始化app
	for _, chatbotCfg := range cfg.Namespace {
		err = cServer.initChatbot(&chatbotCfg)
		if err != nil {
			return CardServer{}, err
		}
	}

	for _, tempCfg := range cfg.Chattemplate {
		err = cServer.initDefaultApp(&tempCfg)
		if err != nil {
			return CardServer{}, err
		}
	}

	for _, app := range cServer.apps {
		if app.appName == "default" {
			// namespace中的default和chattemplate中的“0”共同构成默认对话应用
			if chatbot, ok := cServer.chatbots["0"]; ok {
				cServer.setAppChatbot(app, chatbot)
				break
			}
			return CardServer{}, errors.New("default chatbot not found")
		}
	}

	return cServer, nil
}

// 这里只要传的不对一律用默认的appid，这样是否会造成误解？不如直接报错？
func (cServer *CardServer) CreateNewSessionByApp(appId any) (SessionId, Version, error) {
	var (
		app *application
		ok  bool
	)

	switch v := appId.(type) {
	case AppId:
		app, ok = cServer.apps[v]
		if !ok {
			cServer.logger.Println("appId:[", appId, "] is not exist, use default appid")
			app, ok = cServer.apps[cServer.defaultAppId]
		}
	default:
		cServer.logger.Println("appId:[", appId, "] is not exist, use default appid")
		app, ok = cServer.apps[cServer.defaultAppId]
	}

	if ok {
		//fmt.Println("check app.chatbot")
		//fmt.Println(app.chatbot)
		//fmt.Println(cServer.defaultAppId)
		sessionId, session, version := app.createNewSession()
		cServer.sessions[sessionId] = session
		return sessionId, version, nil
	}

	cServer.logger.Println("default appId is not exist")

	return SessionId{}, Version{}, errors.New("app config error, default app not found")
}

// 外层必须接收这个错误，不能让服务结束
func (cServer *CardServer) FirstStreamChatOnce(sessionId SessionId, VersionId Version, msg string) (modelServer.RecvStreamContext, Version, error) {

	if session, ok := cServer.sessions[sessionId]; ok {
		//session.cardsMap[VersionId].ModelSession.FirstUserMessage(msg)
		//session.cardsMap[VersionId].ModelSession.AddUserMessage(msg)
		return session.firstStreamChatOnce(VersionId, msg)
	}
	cServer.logger.Println("not add chat to session, sessionId not found")
	return modelServer.RecvStreamContext{}, VersionId, errors.New("app config error, sessionId not found")
}

func (cServer *CardServer) NextStreamChatOnce(sessionId SessionId, VersionId Version, msg string) (modelServer.RecvStreamContext, Version, error) {

	if session, ok := cServer.sessions[sessionId]; ok {
		//session.cardsMap[VersionId].ModelSession.FirstUserMessage(msg)
		//session.cardsMap[VersionId].ModelSession.AddUserMessage(msg)
		return session.nextStreamChatOnce(VersionId, msg, OpeDeepen)
	}
	cServer.logger.Println("not add chat to session, sessionId not found")
	return modelServer.RecvStreamContext{}, VersionId, errors.New("app config error, sessionId not found")
}

// todo: 如果碰巧生成了已经有uuid，需要重新生成一个
// todo：通过api创建一个应用，指定提示词，指定chatbot，优先级低，当前全部配置文件写死应用
func (cServer *CardServer) initDefaultApp(tempCfg *config.Chattemplate) error {
	// App的概念是为了能区分不同的系统提示词和用户自定义提示词，但一定要有个默认的appid用于普通对话，
	// 所以在初始化时默认生成个appid，之后用户请求的appid不存在或为空，一律路由到名为default这个0号appid
	appid := AppId{MainId: modelServer.GetNewUUID()}

	var app application = application{
		sessions: make(map[SessionId]*session),

		chatbot: &modelServer.Chatbot{},
		appId:   appid,
		appName: tempCfg.Name,

		chattemplate: modelServer.ChatTemplate{
			Sysprompt:      tempCfg.Template.Sysprompt,
			Userprompt:     tempCfg.Template.Userprompt,
			Tools:          tempCfg.Template.Tools,
			Responseformat: tempCfg.Template.Responseformat,
		},
		createTime: time.Now(),
	}
	cServer.apps[appid] = &app
	if cServer.defaultAppId == (AppId{}) {
		cServer.defaultAppId = appid
	}
	//fmt.Printf(" default app:%v\n", cServer.defaultAppId)
	return nil
}

func (cServer *CardServer) initChatbot(cfs *config.Namespace) error {
	chatbotCfg := openai.DefaultConfig(cfs.Chatbot.APIKey)
	chatbotCfg.BaseURL = cfs.Chatbot.BaseURL

	chatbot := modelServer.Chatbot{
		Cfg:       &chatbotCfg,
		IsStream:  cfs.Chatbot.IsStream,
		MaxTokens: cfs.Chatbot.MaxTokens,
		Model:     cfs.Chatbot.Model,
	}

	err := modelServer.InitChatbot(&chatbot)
	if err != nil {
		return err
	}
	cServer.chatbots[cfs.ChatbotID] = &chatbot
	return nil
}

func (cServer *CardServer) setAppChatbot(app *application, chatbot *modelServer.Chatbot) {
	app.chatbot = chatbot
}

func (s *CardServer) getNode() (*snowflake.Node, error) {
	if s.snowflakeManager == nil {
		return nil, errors.New("snowflake not initialized")
	}
	return s.snowflakeManager, nil
	// snowflakeNode.Int64() → 直接拿 int64
	// snowflakeNode.String() → 转为字符串
	// snowflakeNode.Base2() / id.Base36() → 二进制 / 36 进制
	// snowflakeNode.Time() → 生成 ID 的时间（Go time.Time）
	// snowflakeNode.Node() → 哪个 nodeID 生成的
	// snowflakeNode.Step() → 序列号（同一毫秒内的第几个）
}
