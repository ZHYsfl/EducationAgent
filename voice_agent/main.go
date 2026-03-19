package main

import (
	"fmt"
	"log"
	"net/http"

	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  65536,
	WriteBufferSize: 65536,
	// Allow all origins for development, in production, should set this to the origin of the frontend.
	CheckOrigin: func(r *http.Request) bool { return true },
}

func main() {
	config := LoadConfig()

	log.Printf("Voice Agent server starting on :%d", config.ServerPort)
	log.Printf("  ASR:       %s", config.ASRWSURL)
	log.Printf("  Small LLM: %s (%s)", config.SmallLLMBaseURL, config.SmallLLMModel)
	log.Printf("  Large LLM: %s (%s)", config.LargeLLMBaseURL, config.LargeLLMModel)
	log.Printf("  TTS:       %s", config.TTSURL)

	http.Handle("/", http.FileServer(http.Dir("static")))

	http.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			log.Printf("WebSocket upgrade failed: %v", err)
			return
		}
		log.Printf("New client connected: %s", r.RemoteAddr)

		session := NewSession(conn, config)
		session.Run()

		log.Printf("Client disconnected: %s", r.RemoteAddr)
	})

	addr := fmt.Sprintf(":%d", config.ServerPort)
	if err := http.ListenAndServe(addr, nil); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}
