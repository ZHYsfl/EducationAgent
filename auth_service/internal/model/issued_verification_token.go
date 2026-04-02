package model

type IssuedVerificationToken struct {
	VerificationTokenHash string `gorm:"column:verification_token_hash;primaryKey"`
	ExpiresAt             int64  `gorm:"column:expires_at"`
	CreatedAt             int64  `gorm:"column:created_at"`
}

func (IssuedVerificationToken) TableName() string { return "issued_verification_tokens" }
