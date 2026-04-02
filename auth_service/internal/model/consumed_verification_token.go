package model

type ConsumedVerificationToken struct {
	VerificationTokenHash string `gorm:"column:verification_token_hash;primaryKey"`
	UserID                string `gorm:"column:user_id"`
	ConsumedAt            int64  `gorm:"column:consumed_at"`
}

func (ConsumedVerificationToken) TableName() string { return "consumed_verification_tokens" }
