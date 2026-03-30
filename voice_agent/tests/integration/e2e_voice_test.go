package integration

import (
	"testing"
	"time"

	"github.com/gorilla/websocket"
	agent "voiceagent/agent"
)

// TestE2E_VoiceInteraction 测试完整的语音交互流程
func TestE2E_VoiceInteraction(t *testing.T) {
	srv, wsURL := setupTestServer(t)
	defer srv.Close()

	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	// 1. VAD 检测到语音开始
	sendMsg(t, conn, agent.WSMessage{Type: "vad_start"})
	waitForState(t, conn, "listening", time.Second)

	// 2. 发送音频数据
	for i := 0; i < 10; i++ {
		conn.WriteMessage(websocket.BinaryMessage, []byte{byte(i), byte(i + 1)})
		time.Sleep(10 * time.Millisecond)
	}

	// 3. VAD 检测到语音结束
	sendMsg(t, conn, agent.WSMessage{Type: "vad_end"})
	waitForState(t, conn, "processing", time.Second)

	// 4. 等待 TTS 音频返回
	deadline := time.Now().Add(3 * time.Second)
	audioReceived := false
	for time.Now().Before(deadline) {
		conn.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
		msgType, _, err := conn.ReadMessage()
		if err == nil && msgType == websocket.BinaryMessage {
			audioReceived = true
			break
		}
	}

	if !audioReceived {
		t.Fatal("no audio received from TTS")
	}

	// 5. 验证最终回到 idle
	waitForState(t, conn, "idle", 2*time.Second)
}
