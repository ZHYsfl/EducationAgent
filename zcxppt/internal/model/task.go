package model

import "time"

type Task struct {
	TaskID       string    `json:"task_id"`
	SessionID    string    `json:"session_id,omitempty"`
	Topic        string    `json:"topic,omitempty"`
	Status       string    `json:"status"`
	Progress     int       `json:"progress"`
	CurrentPageID string   `json:"current_viewing_page_id,omitempty"`
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
