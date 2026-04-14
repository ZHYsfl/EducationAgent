package main

import (
	"fmt"
	"log"
	"mime"
	"net/http"
	"strings"

	"voiceagentv2/agent"
	svcclients "voiceagentv2/internal/clients"
	cfgpkg "voiceagentv2/internal/config"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

func init() {
	mime.AddExtensionType(".mjs", "application/javascript")
}

var upgrader = websocket.Upgrader{
	ReadBufferSize:  65536,
	WriteBufferSize: 65536,
	CheckOrigin:     func(r *http.Request) bool { return true },
}

func main() {
	config := cfgpkg.LoadConfig()
	clients := svcclients.NewServiceClients(config)
	agent.SetGlobalClients(clients)

	log.Printf("Voice Agent v2 starting on :%d", config.ServerPort)
	log.Printf("  ASR:       %s", config.ASRWSURL)
	log.Printf("  Small LLM: %s (%s)", config.SmallLLMBaseURL, config.SmallLLMModel)
	log.Printf("  Large LLM: %s (%s)", config.LargeLLMBaseURL, config.LargeLLMModel)
	log.Printf("  TTS:       %s", config.TTSURL)

	r := gin.New()
	r.Use(gin.Recovery())
	r.Use(corsMiddleware())

	// Static files
	r.StaticFS("/models", http.Dir("../models"))
	r.Use(noCacheMiddleware())
	r.StaticFS("/static", http.Dir("static"))
	r.GET("/", func(c *gin.Context) {
		c.File("static/index.html")
	})

	// WebSocket
	r.GET("/ws", wsHandler(config, clients))

	// REST API
	api := r.Group("/api/v1")
	{
		api.GET("/tasks/:task_id/preview", gin.WrapF(agent.HandlePreview))
		api.POST("/files/upload", gin.WrapF(agent.HandleUploadFile))
		api.POST("/voice/ppt_message", gin.WrapF(agent.HandlePPTMessage))
	}

	if err := r.Run(fmt.Sprintf(":%d", config.ServerPort)); err != nil {
		log.Fatalf("server: %v", err)
	}
}

func wsHandler(config *cfgpkg.Config, clients agent.ExternalServices) gin.HandlerFunc {
	return func(c *gin.Context) {
		userID := strings.TrimSpace(c.Query("user_id"))
		if userID == "" {
			c.String(http.StatusBadRequest, "user_id required")
			return
		}
		sessionID := strings.TrimSpace(c.Query("session_id"))
		conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
		if err != nil {
			log.Printf("[ws] upgrade failed: %v", err)
			return
		}
		log.Printf("[ws] connected: %s (session=%s user=%s)", c.ClientIP(), sessionID, userID)
		session, err := agent.NewSession(conn, config, clients, sessionID, userID)
		if err != nil {
			log.Printf("[ws] NewSession: %v", err)
			_ = conn.Close()
			return
		}
		agent.RegisterSession(session)
		session.Run()
		agent.UnregisterSession(session)
		log.Printf("[ws] disconnected: %s (session=%s)", c.ClientIP(), session.SessionID)
	}
}

func corsMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Access-Control-Allow-Origin", "*")
		c.Header("Access-Control-Allow-Headers", "Content-Type, Authorization")
		c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		c.Next()
	}
}

func noCacheMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		p := c.Request.URL.Path
		if strings.HasSuffix(p, ".js") || strings.HasSuffix(p, ".html") || p == "/" {
			c.Header("Cache-Control", "no-cache, no-store, must-revalidate")
			c.Header("Pragma", "no-cache")
			c.Header("Expires", "0")
		}
		c.Next()
	}
}
