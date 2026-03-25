package model

type ExportRequest struct {
	TaskID string `json:"task_id"`
	Format string `json:"format"`
}

type ExportCreateResponse struct {
	ExportID          string `json:"export_id"`
	Status            string `json:"status"`
	EstimatedSeconds  int    `json:"estimated_seconds"`
}

type ExportStatusResponse struct {
	ExportID    string `json:"export_id"`
	Status      string `json:"status"`
	DownloadURL string `json:"download_url,omitempty"`
	Format      string `json:"format"`
	FileSize    int64  `json:"file_size,omitempty"`
	Error       string `json:"error,omitempty"`
}

type ExportJob struct {
	ExportID    string `json:"export_id"`
	TaskID      string `json:"task_id"`
	Format      string `json:"format"`
	Status      string `json:"status"`
	DownloadURL string `json:"download_url,omitempty"`
	Progress    int    `json:"progress"`
	FileSize    int64  `json:"file_size,omitempty"`
	Error       string `json:"error,omitempty"`
	CreatedAt   int64  `json:"created_at"`
	UpdatedAt   int64  `json:"updated_at"`
}
