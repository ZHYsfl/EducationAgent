package integration

import (
	"testing"
	"time"

	"github.com/gorilla/websocket"
	agent "voiceagent/agent"
)

// TestE2E_PPTWorkflow 测试完整的 PPT 生成和修改流程
func TestE2E_PPTWorkflow(t *testing.T) {
	srv, wsURL := setupTestServer(t)
	defer srv.Close()

	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	// 1. 创建任务
	sendMsg(t, conn, agent.WSMessage{
		Type:  "task_init",
		Topic: "Python 基础",
	})

	// 2. 等待任务创建完成
	msg := waitForMessageType(t, conn, "ppt_status", 3*time.Second)
	taskID := msg.TaskID
	if taskID == "" {
		t.Fatal("no task_id returned")
	}

	// 3. 等待页面渲染
	msg = waitForMessageType(t, conn, "page_rendered", 5*time.Second)
	if msg.PageID == "" {
		t.Fatal("no page_id in page_rendered")
	}

	// 4. 导航到页面
	sendMsg(t, conn, agent.WSMessage{
		Type:   "page_navigate",
		TaskID: taskID,
		PageID: msg.PageID,
	})

	time.Sleep(100 * time.Millisecond)

	// 5. 发送修改反馈
	sendMsg(t, conn, agent.WSMessage{
		Type: "text_input",
		Text: "把标题改成更醒目的样式",
	})

	// 6. 验证系统响应
	waitForState(t, conn, "processing", time.Second)
	waitForState(t, conn, "idle", 3*time.Second)
}
