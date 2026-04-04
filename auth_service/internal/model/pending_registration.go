package model

type PendingRegistration struct {
	UserID                string `gorm:"column:user_id;primaryKey"`
	Username              string `gorm:"column:username"`
	Email                 string `gorm:"column:email"`
	PasswordHash          string `gorm:"column:password_hash"`
	DisplayName           string `gorm:"column:display_name"`
	Subject               string `gorm:"column:subject"`
	School                string `gorm:"column:school"`
	Role                  string `gorm:"column:role"`
	VerificationTokenHash string `gorm:"column:verification_token_hash"`
	VerificationExpiresAt int64  `gorm:"column:verification_expires_at"`
	VerificationSentAt    int64  `gorm:"column:verification_sent_at"`
	CreatedAt             int64  `gorm:"column:created_at"`
	UpdatedAt             int64  `gorm:"column:updated_at"`
}

func (PendingRegistration) TableName() string { return "pending_registrations" }
