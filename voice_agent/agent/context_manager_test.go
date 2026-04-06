package agent

import (
	"encoding/json"
	"testing"
)

func TestContextManager_Export(t *testing.T) {
	// 创建一个测试 session
	session := &Session{
		SessionID: "test_session_123",
		UserID:    "test_user_456",
		OwnedTasks: map[string]string{
			"task_1": "高等数学",
			"task_2": "大学物理",
		},
		ActiveTaskID:  "task_1",
		ViewingPageID: "page_3",
		PendingQuestions: map[string]PendingQuestion{
			"ctx_1": {
				TaskID:        "task_1",
				PageID:        "page_3",
				QuestionText:  "页面3的标题冲突，请选择：A) 保留原标题 B) 使用新标题",
				BaseTimestamp: 1234567890,
			},
		},
	}

	// 创建 ContextManager
	cm := NewContextManager(session)

	// 导出快照
	snapshot := cm.Export()

	// 验证基本信息
	if snapshot.SessionID != "test_session_123" {
		t.Errorf("Expected SessionID test_session_123, got %s", snapshot.SessionID)
	}
	if snapshot.UserID != "test_user_456" {
		t.Errorf("Expected UserID test_user_456, got %s", snapshot.UserID)
	}

	// 验证任务列表
	if len(snapshot.Tasks) != 2 {
		t.Errorf("Expected 2 tasks, got %d", len(snapshot.Tasks))
	}
	if snapshot.ActiveTaskID != "task_1" {
		t.Errorf("Expected ActiveTaskID task_1, got %s", snapshot.ActiveTaskID)
	}

	// 验证冲突问题
	if len(snapshot.PendingQuestions) != 1 {
		t.Errorf("Expected 1 pending question, got %d", len(snapshot.PendingQuestions))
	}
	if len(snapshot.PendingQuestions) > 0 {
		q := snapshot.PendingQuestions[0]
		if q.TaskID != "task_1" {
			t.Errorf("Expected question TaskID task_1, got %s", q.TaskID)
		}
		if q.PageID != "page_3" {
			t.Errorf("Expected question PageID page_3, got %s", q.PageID)
		}
	}
}

func TestContextManager_ExportJSON(t *testing.T) {
	session := &Session{
		SessionID: "test_session",
		UserID:    "test_user",
		OwnedTasks: map[string]string{
			"task_1": "测试课件",
		},
	}

	cm := NewContextManager(session)
	jsonStr, err := cm.ExportJSON()
	if err != nil {
		t.Fatalf("ExportJSON failed: %v", err)
	}

	// 验证 JSON 可以解析
	var snapshot ContextSnapshot
	if err := json.Unmarshal([]byte(jsonStr), &snapshot); err != nil {
		t.Fatalf("Failed to unmarshal JSON: %v", err)
	}

	if snapshot.SessionID != "test_session" {
		t.Errorf("Expected SessionID test_session, got %s", snapshot.SessionID)
	}
}

func TestDiff(t *testing.T) {
	before := ContextSnapshot{
		SessionID:    "test",
		ActiveTaskID: "task_1",
		Tasks:        []TaskInfo{{TaskID: "task_1", Topic: "数学"}},
	}

	after := ContextSnapshot{
		SessionID:    "test",
		ActiveTaskID: "task_2",
		Tasks: []TaskInfo{
			{TaskID: "task_1", Topic: "数学"},
			{TaskID: "task_2", Topic: "物理"},
		},
	}

	changes := Diff(before, after)

	// 应该检测到任务数量变化
	foundTasksChange := false
	foundActiveTaskChange := false
	for _, change := range changes {
		if change.Type == "tasks_count_changed" {
			foundTasksChange = true
		}
		if change.Type == "active_task_changed" {
			foundActiveTaskChange = true
		}
	}

	if !foundTasksChange {
		t.Error("Diff should detect tasks count change")
	}
	if !foundActiveTaskChange {
		t.Error("Diff should detect active task change")
	}
}
