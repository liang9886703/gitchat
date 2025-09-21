package main

import (
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
)

// Response represents a standard API response
type Response struct {
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
	Success bool        `json:"success"`
}

// ChatMessage represents a chat message
type ChatMessage struct {
	ID       int    `json:"id"`
	Username string `json:"username"`
	Message  string `json:"message"`
	Time     string `json:"time"`
}

// In-memory storage for chat messages (in production, use a database)
var messages []ChatMessage
var messageCounter int

func main() {
	// Set Gin mode based on environment
	if os.Getenv("GIN_MODE") == "release" {
		gin.SetMode(gin.ReleaseMode)
	}

	// Create Gin router with default middleware (logger and recovery)
	r := gin.Default()

	// CORS middleware
	r.Use(func(c *gin.Context) {
		c.Writer.Header().Set("Access-Control-Allow-Origin", "*")
		c.Writer.Header().Set("Access-Control-Allow-Credentials", "true")
		c.Writer.Header().Set("Access-Control-Allow-Headers", "Content-Type, Content-Length, Accept-Encoding, X-CSRF-Token, Authorization, accept, origin, Cache-Control, X-Requested-With")
		c.Writer.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS, GET, PUT, DELETE")

		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}

		c.Next()
	})

	// Health check endpoint
	r.GET("/health", healthCheck)

	// API routes
	api := r.Group("/api/v1")
	{
		api.GET("/messages", getMessages)
		api.POST("/messages", postMessage)
		api.DELETE("/messages/:id", deleteMessage)
	}

	// Get port from environment or default to 8080
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	// Start server
	r.Run(":" + port)
}

// healthCheck returns the health status of the service
func healthCheck(c *gin.Context) {
	c.JSON(http.StatusOK, Response{
		Message: "GitChat server is running",
		Success: true,
	})
}

// getMessages returns all chat messages
func getMessages(c *gin.Context) {
	c.JSON(http.StatusOK, Response{
		Message: "Messages retrieved successfully",
		Data:    messages,
		Success: true,
	})
}

// postMessage creates a new chat message
func postMessage(c *gin.Context) {
	var newMessage ChatMessage
	
	if err := c.ShouldBindJSON(&newMessage); err != nil {
		c.JSON(http.StatusBadRequest, Response{
			Message: "Invalid message format",
			Success: false,
		})
		return
	}

	// Validate required fields
	if newMessage.Username == "" || newMessage.Message == "" {
		c.JSON(http.StatusBadRequest, Response{
			Message: "Username and message are required",
			Success: false,
		})
		return
	}

	// Set ID and add to messages
	messageCounter++
	newMessage.ID = messageCounter
	
	// Set current time if not provided
	if newMessage.Time == "" {
		newMessage.Time = getCurrentTime()
	}

	messages = append(messages, newMessage)

	c.JSON(http.StatusCreated, Response{
		Message: "Message created successfully",
		Data:    newMessage,
		Success: true,
	})
}

// deleteMessage removes a message by ID
func deleteMessage(c *gin.Context) {
	id := c.Param("id")
	
	for i, message := range messages {
		if message.ID == parseID(id) {
			// Remove message from slice
			messages = append(messages[:i], messages[i+1:]...)
			c.JSON(http.StatusOK, Response{
				Message: "Message deleted successfully",
				Success: true,
			})
			return
		}
	}

	c.JSON(http.StatusNotFound, Response{
		Message: "Message not found",
		Success: false,
	})
}

// Helper functions
func getCurrentTime() string {
	return time.Now().Format("2006-01-02 15:04:05")
}

func parseID(idStr string) int {
	id, err := strconv.Atoi(idStr)
	if err != nil {
		return 0
	}
	return id
}