package model

type Session struct {
	ID        string `gorm:"column:id;primaryKey"`
	UserID    string `gorm:"column:user_id"`
	Title     string `gorm:"column:title"`
	Status    string `gorm:"column:status"`
	CreatedAt int64  `gorm:"column:created_at"`
	UpdatedAt int64  `gorm:"column:updated_at"`
}

func (Session) TableName() string { return "sessions" }
