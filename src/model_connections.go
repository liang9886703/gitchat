package router

import (
	"gitchat/src/cards"
	"gitchat/src/config"
	"github.com/gin-gonic/gin"
	"net"
	"net/http"
)

func localModelAccess(c *gin.Context, configurations []*config.Config) bool {
	if len(configurations) == 0 || configurations[0] == nil {
		return false
	}
	host, _, err := net.SplitHostPort(configurations[0].ListenAddr)
	if err != nil {
		return false
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return false
	}
	remote, _, err := net.SplitHostPort(c.Request.RemoteAddr)
	if err != nil {
		return false
	}
	ip = net.ParseIP(remote)
	if ip == nil || !ip.IsLoopback() {
		return false
	}
	host, _, err = net.SplitHostPort(c.Request.Host)
	if err != nil {
		host = c.Request.Host
	}
	ip = net.ParseIP(host)
	return host == "localhost" || ip != nil && ip.IsLoopback()
}
func registerModels(r *gin.Engine, server *cards.CardServer, configurations []*config.Config) {
	r.GET("/api/model-connections", func(c *gin.Context) { c.JSON(200, server.ModelConnections(c.GetString("owner"))) })
	r.POST("/api/model-connections", func(c *gin.Context) {
		var in cards.ConnectionInput
		if !bind(c, &in) {
			return
		}
		if in.ID != "" {
			c.JSON(400, gin.H{"error": "创建连接不能指定 ID"})
			return
		}
		value, err := server.SaveModelConnection(c.GetString("owner"), in, localModelAccess(c, configurations))
		respond(c, 201, value, err)
	})
	r.PUT("/api/model-connections/:id", func(c *gin.Context) {
		var in cards.ConnectionInput
		if !bind(c, &in) {
			return
		}
		in.ID = c.Param("id")
		value, err := server.SaveModelConnection(c.GetString("owner"), in, localModelAccess(c, configurations))
		respond(c, 200, value, err)
	})
	r.DELETE("/api/model-connections/:id", func(c *gin.Context) {
		var in struct {
			Revision int `json:"revision"`
		}
		if !bind(c, &in) {
			return
		}
		value, err := server.DeleteModelConnection(c.GetString("owner"), c.Param("id"), in.Revision)
		respond(c, 200, value, err)
	})
	r.PUT("/api/model-selection", func(c *gin.Context) {
		var in struct {
			ID       string `json:"id"`
			Revision int    `json:"revision"`
		}
		if !bind(c, &in) {
			return
		}
		value, err := server.SelectModel(c.GetString("owner"), in.ID, in.Revision)
		respond(c, 200, value, err)
	})
	probes := make(chan struct{}, 4)
	for _, operation := range []string{"test", "models"} {
		operation := operation
		r.POST("/api/model-connections/"+operation, func(c *gin.Context) {
			select {
			case probes <- struct{}{}:
				defer func() { <-probes }()
			default:
				c.JSON(http.StatusTooManyRequests, gin.H{"error": "连接测试繁忙，请稍后重试"})
				return
			}
			var in cards.ConnectionInput
			if !bind(c, &in) {
				return
			}
			value, err := server.ProbeModel(c.Request.Context(), c.GetString("owner"), in, localModelAccess(c, configurations), operation == "models")
			if err != nil {
				c.JSON(400, gin.H{"error": err.Error()})
				return
			}
			c.JSON(200, value)
		})
	}
}
