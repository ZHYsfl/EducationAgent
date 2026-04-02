package model

type MemoryEntry struct {
	ID              string  `json:"memory_id" gorm:"column:id;primaryKey"`
	UserID          string  `json:"user_id" gorm:"column:user_id"`
	Category        string  `json:"category" gorm:"column:category"`
	Key             string  `json:"key" gorm:"column:key"`
	Value           string  `json:"value" gorm:"column:value"`
	Context         string  `json:"context" gorm:"column:context"`
	Confidence      float64 `json:"confidence" gorm:"column:confidence"`
	Source          string  `json:"source" gorm:"column:source"`
	SourceSessionID *string `json:"source_session_id,omitempty" gorm:"column:source_session_id"`
	CreatedAt       int64   `json:"created_at" gorm:"column:created_at"`
	UpdatedAt       int64   `json:"updated_at" gorm:"column:updated_at"`
}

func (MemoryEntry) TableName() string { return "memory_entries" }
