package router

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"gitchat/src/cards"
	"gitchat/src/config"
	"gitchat/web"
	"github.com/gin-gonic/gin"
)

func GetCardsServer() (*cards.CardServer, error) {
	cfg, err := config.GetConfig()
	if err != nil {
		return nil, err
	}
	server, err := cards.CreateCardServer(cfg)
	return &server, err
}

// SetupRouter serves the browser application and both API generations.
func SetupRouter(server *cards.CardServer, configurations ...*config.Config) *gin.Engine {
	r := gin.New()
	r.Use(gin.Recovery())
	mode := "demo"
	if len(configurations) > 0 {
		mode = configurations[0].Mode
	}
	r.Use(func(c *gin.Context) {
		c.Header("X-Content-Type-Options", "nosniff")
		c.Header("Referrer-Policy", "same-origin")
		c.Header("X-Frame-Options", "DENY")
		c.Header("Cache-Control", "no-store")
		if c.Request.Method != http.MethodGet && c.Request.Method != http.MethodHead && c.Request.Method != http.MethodOptions {
			if origin := c.GetHeader("Origin"); origin != "" {
				u, err := url.Parse(origin)
				if err != nil || u.Host != c.Request.Host {
					c.AbortWithStatusJSON(403, gin.H{"error": "跨站请求被拒绝"})
					return
				}
			}
			if c.GetHeader("Sec-Fetch-Site") == "cross-site" {
				c.AbortWithStatusJSON(403, gin.H{"error": "跨站请求被拒绝"})
				return
			}
			c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 1<<20)
		}
		owner, err := c.Cookie("gitchat_owner")
		decoded, decodeErr := hex.DecodeString(owner)
		if err != nil || decodeErr != nil || len(decoded) != 32 {
			owner, err = newOwner()
			if err != nil {
				c.AbortWithStatus(500)
				return
			}
			setOwnerCookie(c, owner)
		}
		c.Set("owner", owner)
		c.Next()
	})
	r.GET("/health", func(c *gin.Context) { c.JSON(200, gin.H{"status": "ok"}) })
	registerDevices(r)
	r.GET("/api/config", func(c *gin.Context) {
		c.JSON(200, gin.H{"mode": mode, "model_configured": mode != "demo", "apps": server.Apps()})
	})
	r.GET("/api/sessions", func(c *gin.Context) { c.JSON(200, server.ListSessions(c.GetString("owner"))) })
	r.POST("/api/sessions", func(c *gin.Context) {
		var body struct {
			AppID string `json:"app_id"`
			Title string `json:"title"`
		}
		if !bind(c, &body) {
			return
		}
		session, err := server.NewSession(c.GetString("owner"), body.AppID, body.Title)
		respond(c, 201, session, err)
	})
	r.GET("/api/sessions/:id", func(c *gin.Context) {
		s, e := server.GetSession(c.GetString("owner"), c.Param("id"))
		respond(c, 200, s, e)
	})
	r.DELETE("/api/sessions/:id", func(c *gin.Context) {
		respond(c, 200, gin.H{"ok": true}, server.DeleteSession(c.GetString("owner"), c.Param("id")))
	})
	r.GET("/api/sessions/:id/export", func(c *gin.Context) {
		s, e := server.GetSession(c.GetString("owner"), c.Param("id"))
		if e != nil {
			apiError(c, e)
			return
		}
		c.Header("Content-Disposition", `attachment; filename="gitchat-session.json"`)
		c.IndentedJSON(200, s)
	})
	r.POST("/api/sessions/:id/chat", func(c *gin.Context) {
		var input cards.ChatInput
		if !bind(c, &input) {
			return
		}
		events, err := server.Chat(c.Request.Context(), c.GetString("owner"), c.Param("id"), input)
		if err != nil {
			apiError(c, err)
			return
		}
		streamHeaders(c)
		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case event, ok := <-events:
				if !ok {
					return
				}
				if !writeEvent(c, "message", event) {
					return
				}
			case <-ticker.C:
				if _, err := fmt.Fprint(c.Writer, ": heartbeat\n\n"); err != nil {
					return
				}
				c.Writer.Flush()
			case <-c.Request.Context().Done():
				return
			}
		}
	})
	r.POST("/api/sessions/:id/cards/:cardID/stop", func(c *gin.Context) {
		respond(c, 200, gin.H{"ok": true}, server.Stop(c.GetString("owner"), c.Param("id"), c.Param("cardID")))
	})
	r.PATCH("/api/sessions/:id/cards/:cardID", func(c *gin.Context) {
		var patch cards.CardPatch
		if !bind(c, &patch) {
			return
		}
		card, err := server.PatchCard(c.GetString("owner"), c.Param("id"), c.Param("cardID"), patch)
		respond(c, 200, card, err)
	})
	r.POST("/api/sessions/:id/focus", func(c *gin.Context) {
		var input struct {
			CardID string `json:"card_id"`
		}
		if !bind(c, &input) {
			return
		}
		respond(c, 200, gin.H{"ok": true}, server.Focus(c.GetString("owner"), c.Param("id"), input.CardID))
	})
	r.POST("/api/sessions/:id/copy", func(c *gin.Context) {
		var input struct {
			CardID string `json:"card_id"`
		}
		if !bind(c, &input) {
			return
		}
		s, e := server.CopySession(c.GetString("owner"), c.Param("id"), input.CardID)
		respond(c, 201, s, e)
	})
	r.POST("/api/sessions/:id/share", func(c *gin.Context) {
		var input struct {
			CardID string `json:"card_id"`
		}
		if !bind(c, &input) {
			return
		}
		token, e := server.Share(c.GetString("owner"), c.Param("id"), input.CardID)
		respond(c, 201, gin.H{"token": token, "url": "/?share=" + url.QueryEscape(token)}, e)
	})
	r.GET("/api/shares/:token", func(c *gin.Context) { s, e := server.GetShare(c.Param("token")); respond(c, 200, s, e) })
	r.POST("/api/shares/:token/import", func(c *gin.Context) {
		s, e := server.ImportShare(c.GetString("owner"), c.Param("token"))
		respond(c, 201, s, e)
	})
	r.GET("/api/preferences", func(c *gin.Context) { c.JSON(200, server.GetPreferences(c.GetString("owner"))) })
	r.PUT("/api/preferences", func(c *gin.Context) {
		var p cards.Preferences
		if !bind(c, &p) {
			return
		}
		respond(c, 200, p, server.SetPreferences(c.GetString("owner"), p))
	})
	r.GET("/api/stats", func(c *gin.Context) { c.JSON(200, server.Stats(c.GetString("owner"))) })
	r.POST("/g/:appid", legacyChat(server))
	r.POST("/g/:appid/:session", legacyChat(server))
	assets, _ := fs.Sub(web.FS, ".")
	files := http.FileServer(http.FS(assets))
	r.NoRoute(func(c *gin.Context) {
		if c.Request.Method != http.MethodGet && c.Request.Method != http.MethodHead {
			c.Status(404)
			return
		}
		switch c.Request.URL.Path {
		case "/sw.js":
			c.Header("Cache-Control", "no-cache")
			c.Header("Service-Worker-Allowed", "/")
			files.ServeHTTP(c.Writer, c.Request)
		case "/manifest.webmanifest":
			c.Header("Content-Type", "application/manifest+json")
			files.ServeHTTP(c.Writer, c.Request)
		case "/", "/index.html", "/app.js", "/style.css", "/styles.css", "/platform.js", "/platform.css", "/offline.html",
			"/icons/icon.svg", "/icons/icon-192.png", "/icons/icon-512.png", "/icons/maskable-512.png", "/icons/apple-touch-icon.png":
			files.ServeHTTP(c.Writer, c.Request)
		default:
			c.Status(404)
		}
	})
	return r
}

func bind(c *gin.Context, v any) bool {
	decoder := json.NewDecoder(c.Request.Body)
	if err := decoder.Decode(v); err != nil {
		c.JSON(400, gin.H{"error": "请求需要有效的 JSON 内容"})
		return false
	}
	// A second JSON value is never part of one API request.
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		c.JSON(400, gin.H{"error": "请求只能包含一个 JSON 对象"})
		return false
	}
	return true
}
func respond(c *gin.Context, status int, v any, err error) {
	if err != nil {
		apiError(c, err)
		return
	}
	c.JSON(status, v)
}
func apiError(c *gin.Context, err error) {
	status := 500
	switch {
	case errors.Is(err, cards.ErrNotFound):
		status = 404
	case errors.Is(err, cards.ErrInvalid):
		status = 400
	case errors.Is(err, cards.ErrConflict):
		status = 409
	}
	if status == 500 {
		c.JSON(status, gin.H{"error": "操作失败，请检查服务器日志或重试"})
		log.Printf("API error: %v", err)
		return
	}
	c.JSON(status, gin.H{"error": err.Error()})
}
func streamHeaders(c *gin.Context) {
	c.Header("Content-Type", "text/event-stream; charset=utf-8")
	c.Header("Cache-Control", "no-cache")
	c.Header("X-Accel-Buffering", "no")
}
func writeEvent(c *gin.Context, name string, value any) bool {
	data, err := json.Marshal(value)
	if err != nil {
		return false
	}
	if _, err = fmt.Fprintf(c.Writer, "event: %s\ndata: %s\n\n", name, data); err != nil {
		return false
	}
	c.Writer.Flush()
	return true
}

type legacyRequest struct {
	Version  cards.Version   `json:"version"`
	Session  cards.SessionId `json:"session"`
	IfFirst  bool            `json:"ifFirst"`
	Stream   bool            `json:"stream"`
	Action   string          `json:"action"`
	ParentID string          `json:"parentId"`
	Message  struct {
		ReqID   string `json:"reqId"`
		Content []struct {
			Text []string `json:"text"`
		} `json:"content"`
	} `json:"message"`
}

func legacyChat(server *cards.CardServer) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req legacyRequest
		if !bind(c, &req) {
			return
		}
		var parts []string
		for _, content := range req.Message.Content {
			parts = append(parts, content.Text...)
		}
		question := strings.TrimSpace(strings.Join(parts, "\n"))
		if question == "" {
			c.JSON(400, gin.H{"error": "message.content[].text 不能为空"})
			return
		}
		owner := c.GetString("owner")
		appID := c.Param("appid")
		apps := server.Apps()
		found := false
		for _, app := range apps {
			if app.ID == appID {
				found = true
				break
			}
		}
		if !found && len(apps) > 0 {
			appID = apps[0].ID
		}
		sessionID := c.Param("session")
		if sessionID == "" {
			sessionID = req.Session.MainId
		}
		var session cards.SessionView
		var err error
		if req.IfFirst || sessionID == "" {
			session, err = server.NewSession(owner, appID, question)
		} else {
			session, err = server.GetSession(owner, sessionID)
		}
		if err != nil {
			apiError(c, err)
			return
		}
		// Legacy default app aliases are accepted, explicit applications remain isolated.
		if appID != session.AppID {
			c.JSON(404, gin.H{"error": "session does not belong to application"})
			return
		}
		parent := req.Version.MainId
		if parent == "" {
			parent = req.ParentID
		}
		if parent == "" {
			parent = session.RootID
		}
		operation := req.Action
		if operation == "" || operation == "next" {
			operation = "deepen"
		}
		events, err := server.Chat(c.Request.Context(), owner, session.ID, cards.ChatInput{Operation: operation, ParentIDs: []string{parent}, Question: question, RequestID: req.Message.ReqID})
		if err != nil {
			apiError(c, err)
			return
		}
		if req.Stream {
			streamHeaders(c)
		}
		var current *cards.Card
		for event := range events {
			if event.Card != nil {
				current = event.Card
			}
			version := ""
			status := "in_progress"
			content := event.Delta
			if current != nil {
				version = current.ID
				status = current.Status
			}
			if event.Type == "completed" && !req.Stream && current != nil {
				content = current.Answer
			}
			response := gin.H{"appid": cards.AppId{MainId: session.AppID}, "session": cards.SessionId{MainId: session.ID}, "version": cards.Version{MainId: version}, "message": gin.H{"status": status, "content": gin.H{"type": "text", "text": []string{content}}}, "error": event.Error}
			if req.Stream {
				if !writeEvent(c, "stream", response) {
					return
				}
			} else if event.Type == "completed" || event.Type == "error" || event.Type == "cancelled" {
				c.JSON(200, response)
				return
			}
		}
	}
}
