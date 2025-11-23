package router

import (
	"encoding/json"
	"fmt"
	"gitchat/src/cards"
	"gitchat/src/config"
	"gitchat/src/modelServer"
	"net/http"
	"time"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
)

type msgContent struct {
	ContentType string   `json:"type,omitempty"`
	Parts       []string `json:"text,omitempty"`
}

type messages struct {
	Role       string       `json:"role,omitempty"`
	Content    []msgContent `json:"content,omitempty"`
	CreateTime string       `json:"createTime,omitempty"`
	ReqID      string       `json:"reqId,omitempty"`
}

// 通过对chatgpt抓包后选了一些字段放这
type userRequest struct {
	AppID   cards.AppId     `json:"appid,omitempty"`
	Session cards.SessionId `json:"session,omitempty"`
	Version cards.Version   `json:"version,omitempty"`

	Action  string `json:"action,omitempty"`
	Stream  bool   `json:"stream,omitempty"`
	IfFirst bool   `json:"ifFirst,omitempty"`

	Message  messages `json:"message,omitempty"`
	Model    string   `json:"model,omitempty"`
	ParentID string   `json:"parentId,omitempty"`
	Timezone string   `json:"timezone,omitempty"`

	Language string `json:"languages,omitempty"`
}

type streamMessage struct {
	ID         string     `json:"id,omitempty"`
	CreateTime string     `json:"create_time,omitempty"`
	UpdateTime string     `json:"update_time,omitempty"`
	Content    msgContent `json:"content,omitempty"`
	Status     string     `json:"status,omitempty"` // "in_progress", "completed" 等
}

type userResponse struct {
	Message *streamMessage  `json:"message,omitempty"`
	AppID   cards.AppId     `json:"appid,omitempty"`
	Session cards.SessionId `json:"session,omitempty"`
	Version cards.Version   `json:"version,omitempty"`
	Error   string          `json:"error,omitempty"` // 错误信息，null 或字符串
}

func GetCardsServer() (*cards.CardServer, error) {
	configs, err := config.GetConfig()
	if err != nil {
		return nil, err
	}
	cardsServer, err := cards.CreateCardServer(configs)
	if err != nil {
		return nil, err
	}
	return &cardsServer, nil
}

func buildStreamMessage(msg modelServer.StreamMessage, req userRequest, sessionId cards.SessionId, version cards.Version) (userResponse, error) {

	return userResponse{
		Message: &streamMessage{
			ID:         "",
			CreateTime: "",
			UpdateTime: time.Now().Format("2006-01-02 15:04:05"), //todo
			Status:     "success",                                //todo
			Content: msgContent{
				ContentType: "text",
				Parts:       []string{msg.Message},
			},
		},
		AppID:   req.AppID,
		Session: sessionId,
		Version: version,
		Error:   "",
	}, nil
}

func buildErrorMessage(err error, req userRequest, version cards.Version) userResponse {
	return userResponse{
		Error:   fmt.Sprintf("error: %v", err),
		AppID:   req.AppID,
		Session: req.Session,
		Version: version,
	}
}

func sendStreamMessage(c *gin.Context, ctx modelServer.RecvStreamContext, req userRequest, sessionId cards.SessionId, version cards.Version) {
	c.Writer.Header().Set("Content-Type", "text/event-stream")
	c.Writer.Header().Set("Cache-Control", "no-cache")
	c.Writer.Header().Set("Connection", "keep-alive")

	// 使用 Flusher 确保实时发送数据
	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		c.String(http.StatusInternalServerError, "Streaming unsupported!")
		return
	}

	var res userResponse
	var err error
	for content := range *ctx.Ch {
		fmt.Printf("start receive stream message")
		fmt.Printf("content: %v", content)
		switch content.Type {
		case modelServer.SendStreamContextTypeStream:
			c.SSEvent("stream", res)
			res, err = buildStreamMessage(content, req, sessionId, version)
			//fmt.Printf("%s", content.Message)
		case modelServer.SendStreamContextTypeEnd:
			//fmt.Printf("%s\n", content.Message)
			c.SSEvent("stream", res)
			return
			//res, err = buildStreamMessage(content, req, version)
		case modelServer.SendStreamContextTypeError:
			//fmt.Printf("%s\n", content.Message)
			res = buildErrorMessage(err, req, version)
		default:
			//fmt.Printf("%s\n", content.Message)
			res = buildErrorMessage(fmt.Errorf("unknown stream message type: %v", content.Type), req, version)
		}
		fmt.Printf("stream: %v", res)
		fmt.Fprintf(c.Writer, "stream: %v", res)
		flusher.Flush() // 立即发送到客户端
	}
}

func firstChat(c *gin.Context, req userRequest, cardsServer *cards.CardServer) {
	var (
		ctx   modelServer.RecvStreamContext
		appId cards.AppId = req.AppID
	)

	// 提供了appid，但是没有sessionid，则创建一个新session
	sessionId, version, err := cardsServer.CreateNewSessionByApp(appId)
	if err != nil {
		return
	}
	fmt.Printf("创建一个新session,appId: %v,sessionId: %v,version: %v\n", appId, sessionId, version)
	//todo暂时只支持文字
	ctx, version, err = cardsServer.FirstStreamChatOnce(sessionId, version, req.Message.Content[0].Parts[0])
	if err != nil {
		return
	}

	if req.Stream {
		sendStreamMessage(c, ctx, req, sessionId, version)
	} else {
		sendStreamMessage(c, ctx, req, sessionId, version)
	}

}

func nextChat(c *gin.Context, req userRequest, cardsServer *cards.CardServer) {
	var (
		ctx       modelServer.RecvStreamContext
		appId     cards.AppId     = req.AppID
		sessionId cards.SessionId = req.Session
		version   cards.Version   = req.Version
		err       error
	)

	if sessionId.IsEmpty() {
		res := buildErrorMessage(fmt.Errorf("sessionId is empty"), req, version)
		c.JSON(http.StatusBadRequest, res)
		return
	}

	if version.IsEmpty() {
		res := buildErrorMessage(fmt.Errorf("version is empty"), req, version)
		c.JSON(http.StatusBadRequest, res)
		return
	}

	fmt.Printf("继续当前session的对话,appId: %v,sessionId: %v,version: %v\n", appId, sessionId, version)
	// 提供了appid和sessionid，则继续当前session的对话
	ctx, version, err = cardsServer.NextStreamChatOnce(sessionId, version, req.Message.Content[0].Parts[0])
	if err != nil {
		return
	}

	if req.Stream {
		sendStreamMessage(c, ctx, req, sessionId, version)
	} else {
		sendStreamMessage(c, ctx, req, sessionId, version)
	}

}

func SetupRouter(cardsServer *cards.CardServer) *gin.Engine {
	r := gin.Default()

	// 配置 CORS 中间件
	r.Use(cors.New(cors.Config{
		AllowOrigins:     []string{"*"},
		AllowMethods:     []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowHeaders:     []string{"Origin", "Content-Type", "Accept", "Authorization"},
		ExposeHeaders:    []string{"Content-Length"},
		AllowCredentials: true,
		MaxAge:           12 * time.Hour,
	}))

	r.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	//区分没有session的和有的两个路由
	r.POST("/g/:appid", func(c *gin.Context) {

		req := userRequest{}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		reqStr, err := json.Marshal(req)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		fmt.Printf("receive request: %s\n", string(reqStr))

		if req.IfFirst {
			req.AppID = cards.AppIdFromString(c.Param("appid"))
			firstChat(c, req, cardsServer)
		} else {
			req.AppID = cards.AppIdFromString(c.Param("appid"))
			req.Session = cards.SessionIdFromString(c.Param("session"))
			nextChat(c, req, cardsServer)
		}
	})

	r.POST("/g/:appid/:session", func(c *gin.Context) {

		req := userRequest{}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		req.AppID = cards.AppIdFromString(c.Param("appid"))
		req.Session = cards.SessionIdFromString(c.Param("session"))

		reqStr, err := json.Marshal(req)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		fmt.Printf("receive request: %s\n", string(reqStr))

		nextChat(c, req, cardsServer)
	})
	return r
}
