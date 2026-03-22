package main

import (
	"fmt"
	"log"
	"mime"
	"net/http"
	"strings"

	"github.com/gorilla/websocket"
)

func init() {
	mime.AddExtensionType(".mjs", "application/javascript")
}

func noCacheHandler(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, ".js") || strings.HasSuffix(r.URL.Path, ".html") || r.URL.Path == "/" {
			w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
			w.Header().Set("Pragma", "no-cache")
			w.Header().Set("Expires", "0")
		}
		h.ServeHTTP(w, r)
	})
}

var upgrader = websocket.Upgrader{
	ReadBufferSize:  65536,
	WriteBufferSize: 65536,
	// Allow all origins for development, in production, should set this to the origin of the frontend.
	CheckOrigin: func(r *http.Request) bool { return true },
}

func main() {
	config := LoadConfig()
	clients := NewServiceClients(config)
	setGlobalClients(clients)

	log.Printf("Voice Agent server starting on :%d", config.ServerPort)
	if config.ASRMode == "remote" {
		log.Printf("  ASR:       remote (Doubao, resource=%s)", config.DouBaoASRResourceId)
	} else {
		log.Printf("  ASR:       local (%s)", config.ASRWSURL)
	}
	log.Printf("  Small LLM: %s (%s)", config.SmallLLMBaseURL, config.SmallLLMModel)
	log.Printf("  Large LLM: %s (%s)", config.LargeLLMBaseURL, config.LargeLLMModel)
	if config.TTSMode == "remote" {
		log.Printf("  TTS:       remote (Doubao, voice=%s)", config.DouBaoTTSVoiceType)
	} else {
		log.Printf("  TTS:       local (%s)", config.TTSURL)
	}

	http.Handle("/models/", http.StripPrefix("/models/", http.FileServer(http.Dir("../models"))))
	http.Handle("/", noCacheHandler(http.FileServer(http.Dir("static"))))
	http.HandleFunc("POST /api/v1/upload", withCORS(handleUpload))
	http.HandleFunc("OPTIONS /api/v1/upload", withCORS(preflightOnly))
	http.HandleFunc("GET /api/v1/tasks/{task_id}/preview", withCORS(handlePreview))
	http.HandleFunc("OPTIONS /api/v1/tasks/{task_id}/preview", withCORS(preflightOnly))
	http.HandleFunc("POST /api/v1/voice/ppt_message", withCORS(handlePPTMessage))
	http.HandleFunc("OPTIONS /api/v1/voice/ppt_message", withCORS(preflightOnly))
	http.HandleFunc("POST /api/v1/voice/ppt_message_tool", withCORS(handlePPTMessage))
	http.HandleFunc("OPTIONS /api/v1/voice/ppt_message_tool", withCORS(preflightOnly))

	http.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			log.Printf("WebSocket upgrade failed: %v", err)
			return
		}

		sessionID := r.URL.Query().Get("session_id")
		userID := r.URL.Query().Get("user_id")
		log.Printf("New client connected: %s (session_id=%s, user_id=%s)", r.RemoteAddr, sessionID, userID)

		session := NewSession(conn, config, clients, sessionID, userID)
		registerSession(session)
		session.Run()
		unregisterSession(session)

		log.Printf("Client disconnected: %s (session_id=%s)", r.RemoteAddr, session.SessionID)
	})

	log.Println("Static files served with no-cache headers")

	addr := fmt.Sprintf(":%d", config.ServerPort)
	if err := http.ListenAndServe(addr, nil); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}

func withCORS(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next(w, r)
	}
}

func preflightOnly(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusNoContent)
}
