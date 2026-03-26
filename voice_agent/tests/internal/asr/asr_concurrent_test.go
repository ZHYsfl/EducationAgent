package asr_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	asrpkg "voiceagent/internal/asr"
)

func TestASRClient_ConcurrentReadResult(t *testing.T) {
	srv := mockASRServer(func(conn *websocket.Conn) {
		conn.ReadMessage()
		for i := 0; i < 50; i++ {
			result := asrpkg.ASRResult{Text: "concurrent", IsFinal: false}
			conn.WriteJSON(result)
			time.Sleep(5 * time.Millisecond)
		}
	})
	defer srv.Close()

	c := asrpkg.NewASRClient(wsURL(srv))
	c.StartSession(context.Background())
	defer c.EndSession(context.Background())

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 5; j++ {
				_, err := c.ReadResult(context.Background())
				if err != nil {
					return
				}
			}
		}()
	}
	wg.Wait()
}

func TestASRClient_ConcurrentSendAudio(t *testing.T) {
	var mu sync.Mutex
	audioCount := 0
	srv := mockASRServer(func(conn *websocket.Conn) {
		conn.ReadMessage()
		for {
			_, _, err := conn.ReadMessage()
			if err != nil {
				break
			}
			mu.Lock()
			audioCount++
			mu.Unlock()
		}
	})
	defer srv.Close()

	c := asrpkg.NewASRClient(wsURL(srv))
	c.StartSession(context.Background())
	defer c.EndSession(context.Background())

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				audio := []byte{byte(id), byte(j)}
				c.SendAudio(context.Background(), audio)
			}
		}(i)
	}
	wg.Wait()
	time.Sleep(100 * time.Millisecond)
}

func TestASRClient_ConcurrentStartEndSession(t *testing.T) {
	srv := mockASRServer(func(conn *websocket.Conn) {
		conn.ReadMessage()
		time.Sleep(50 * time.Millisecond)
	})
	defer srv.Close()

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c := asrpkg.NewASRClient(wsURL(srv))
			if err := c.StartSession(context.Background()); err != nil {
				return
			}
			time.Sleep(10 * time.Millisecond)
			c.EndSession(context.Background())
		}()
	}
	wg.Wait()
}

func TestASRClient_ConcurrentContextCancel(t *testing.T) {
	srv := mockASRServer(func(conn *websocket.Conn) {
		conn.ReadMessage()
		time.Sleep(2 * time.Second)
	})
	defer srv.Close()

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c := asrpkg.NewASRClient(wsURL(srv))
			c.StartSession(context.Background())
			ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
			defer cancel()
			c.ReadResult(ctx)
		}()
	}
	wg.Wait()
}

