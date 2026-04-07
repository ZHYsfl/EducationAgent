package model

import "time"

type Task struct {
	TaskID       string    `json:"task_id"`
	SessionID    string    `json:"session_id,omitempty"`
	Topic        string    `json:"topic,omitempty"`
	Status       string    `json:"status"`
	Progress     int       `json:"progress"`
	CurrentPageID string   `json:"current_viewing_page_id,omitempty"`
	PlanID       string    `json:"plan_id,omitempty"`             // 关联的教案任务 ID
	ContentResultID string `json:"content_result_id,omitempty"`   // 关联的内容多样性任务 ID
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type CreateTaskRequest struct {
	SessionID string `json:"session_id"`
	Topic     string `json:"topic"`
}

type UpdateTaskStatusRequest struct {
	Status   string `json:"status"`
	Progress int    `json:"progress"`
}
