package server

type APIResponse struct {
	Code    int         `json:"code"`
	Message string      `json:"message,omitempty"`
	Data    interface{} `json:"data,omitempty"`
}

type SessionModel struct {
	ID        string `gorm:"primaryKey;column:id;size:64"`
	UserID    string `gorm:"column:user_id;size:64;not null;index:idx_sessions_user"`
	Title     string `gorm:"column:title;size:256;default:''"`
	Status    string `gorm:"column:status;size:16;default:'active'"`
	CreatedAt int64  `gorm:"column:created_at;not null"`
	UpdatedAt int64  `gorm:"column:updated_at;not null"`
}

func (SessionModel) TableName() string { return "sessions" }

type FileModel struct {
	ID         string  `gorm:"primaryKey;column:id;size:64"`
	UserID     string  `gorm:"column:user_id;size:64;not null;index:idx_files_user"`
	SessionID  *string `gorm:"column:session_id;size:64;index:idx_files_session"`
	TaskID     *string `gorm:"column:task_id;size:64;index:idx_files_task"`
	Filename   string  `gorm:"column:filename;size:256;not null"`
	FileType   string  `gorm:"column:file_type;size:16;not null"`
	FileSize   int64   `gorm:"column:file_size;not null"`
	StorageURL string  `gorm:"column:storage_url;size:1024;not null"`
	ObjectKey  string  `gorm:"column:object_key;size:1024;not null"`
	Purpose    string  `gorm:"column:purpose;size:32;default:'reference'"`
	CreatedAt  int64   `gorm:"column:created_at;not null"`
}

func (FileModel) TableName() string { return "files" }

type FileDeleteJobModel struct {
	ID         string `gorm:"primaryKey;column:id;size:64"`
	FileID     string `gorm:"column:file_id;size:64;not null;index:idx_file_delete_jobs_file_id"`
	StorageURL string `gorm:"column:storage_url;size:1024;not null"`
	ObjectKey  string `gorm:"column:object_key;size:1024;not null"`
	Status     string `gorm:"column:status;size:16;not null;default:'pending';index:idx_file_delete_jobs_status"`
	RetryCount int    `gorm:"column:retry_count;not null;default:0"`
	LastError  string `gorm:"column:last_error;type:text;default:''"`
	CreatedAt  int64  `gorm:"column:created_at;not null"`
	UpdatedAt  int64  `gorm:"column:updated_at;not null"`
}

func (FileDeleteJobModel) TableName() string { return "file_delete_jobs" }

type SearchRequestModel struct {
	RequestID string `gorm:"primaryKey;column:request_id;size:64"`
	UserID    string `gorm:"column:user_id;size:64;not null;index:idx_search_user"`
	Query     string `gorm:"column:query;type:text;not null"`
	Status    string `gorm:"column:status;size:16;not null;index:idx_search_status"`
	Results   string `gorm:"column:results;type:text"`
	Summary   string `gorm:"column:summary;type:text"`
	Duration  int64  `gorm:"column:duration;not null;default:0"`
	CreatedAt int64  `gorm:"column:created_at;not null"`
	UpdatedAt int64  `gorm:"column:updated_at;not null"`
}

func (SearchRequestModel) TableName() string { return "search_requests" }

type SearchResultItem struct {
	Title   string `json:"title"`
	URL     string `json:"url"`
	Snippet string `json:"snippet"`
	Source  string `json:"source"`
}
