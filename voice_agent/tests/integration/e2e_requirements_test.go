package integration

import (
	"testing"
	"time"

	"github.com/gorilla/websocket"
	agent "voiceagent/agent"
)

// TestE2E_RequirementsCollection 测试完整的需求收集流程
func TestE2E_RequirementsCollection(t *testing.T) {
	srv, wsURL := setupTestServer(t)
	defer srv.Close()

	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	// 1. 发起任务初始化
	sendMsg(t, conn, agent.WSMessage{
		Type:  "task_init",
		Topic: "高等数学",
	})

	// 等待状态变为 collecting
	waitForState(t, conn, "idle", 2*time.Second)

	// 2. 模拟用户逐步提供信息
	sendMsg(t, conn, agent.WSMessage{
		Type: "text_input",
		Text: "我要讲微积分的基本概念",
	})

	time.Sleep(100 * time.Millisecond)

	sendMsg(t, conn, agent.WSMessage{
		Type: "text_input",
		Text: "目标是让学生理解导数和积分",
	})

	time.Sleep(100 * time.Millisecond)

	sendMsg(t, conn, agent.WSMessage{
		Type: "text_input",
		Text: "受众是大一新生",
	})

	// 3. 等待系统生成摘要
	msg := waitForMessageType(t, conn, "requirements_summary", 3*time.Second)
	if msg.SummaryText == "" {
		t.Fatal("expected summary text")
	}

	// 4. 用户确认
	sendMsg(t, conn, agent.WSMessage{
		Type: "requirements_confirm",
	})

	// 5. 验证任务创建
	msg = waitForMessageType(t, conn, "ppt_status", 5*time.Second)
	if msg.TaskID == "" {
		t.Fatal("expected task_id after confirmation")
	}
}
