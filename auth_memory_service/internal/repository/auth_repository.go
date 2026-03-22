package repository

import (
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"auth_memory_service/internal/model"
)

var ErrNotFound = errors.New("not found")

type AuthRepository struct {
	db *gorm.DB
}

func NewAuthRepository(db *gorm.DB) *AuthRepository {
	return &AuthRepository{db: db}
}

func (r *AuthRepository) CreateOrUpdatePending(p model.PendingRegistration) error {
	var existing model.PendingRegistration
	err := r.db.Where("user_id = ?", p.UserID).First(&existing).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return r.db.Create(&p).Error
	}
	if err != nil {
		return err
	}
	return r.db.Model(&model.PendingRegistration{}).Where("user_id = ?", p.UserID).Updates(map[string]interface{}{
		"password_hash":           p.PasswordHash,
		"display_name":            p.DisplayName,
		"subject":                 p.Subject,
		"school":                  p.School,
		"role":                    p.Role,
		"verification_token_hash": p.VerificationTokenHash,
		"verification_expires_at": p.VerificationExpiresAt,
		"verification_sent_at":    p.VerificationSentAt,
		"updated_at":              p.UpdatedAt,
	}).Error
}

func (r *AuthRepository) GetUserByUsername(username string) (model.User, error) {
	var user model.User
	err := r.db.Where("username = ?", username).First(&user).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return model.User{}, ErrNotFound
	}
	return user, err
}

func (r *AuthRepository) GetUserByEmail(email string) (model.User, error) {
	var user model.User
	err := r.db.Where("email = ?", email).First(&user).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return model.User{}, ErrNotFound
	}
	return user, err
}

func (r *AuthRepository) GetUserByID(id string) (model.User, error) {
	var user model.User
	err := r.db.Where("id = ?", id).First(&user).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return model.User{}, ErrNotFound
	}
	return user, err
}

func (r *AuthRepository) GetPendingByUsername(username string) (model.PendingRegistration, error) {
	var p model.PendingRegistration
	err := r.db.Where("username = ?", username).First(&p).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return model.PendingRegistration{}, ErrNotFound
	}
	return p, err
}

func (r *AuthRepository) GetPendingByEmail(email string) (model.PendingRegistration, error) {
	var p model.PendingRegistration
	err := r.db.Where("email = ?", email).First(&p).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return model.PendingRegistration{}, ErrNotFound
	}
	return p, err
}

func (r *AuthRepository) GetPendingByTokenHash(hash string) (model.PendingRegistration, error) {
	var p model.PendingRegistration
	var err error
	for i := 0; i < 5; i++ {
		err = r.db.Where("verification_token_hash = ?", hash).First(&p).Error
		if !isLockError(err) {
			break
		}
		time.Sleep(time.Duration((i+1)*3) * time.Millisecond)
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return model.PendingRegistration{}, ErrNotFound
	}
	return p, err
}

func (r *AuthRepository) IsVerificationTokenConsumed(hash string) (bool, error) {
	var count int64
	var err error
	for i := 0; i < 5; i++ {
		err = r.db.Model(&model.ConsumedVerificationToken{}).Where("verification_token_hash = ?", hash).Count(&count).Error
		if !isLockError(err) {
			break
		}
		time.Sleep(time.Duration((i+1)*3) * time.Millisecond)
	}
	return count > 0, err
}

func (r *AuthRepository) RecordIssuedToken(hash string, expiresAt int64, nowMs int64) error {
	row := model.IssuedVerificationToken{
		VerificationTokenHash: hash,
		ExpiresAt:             expiresAt,
		CreatedAt:             nowMs,
	}
	return r.db.Where("verification_token_hash = ?", hash).Assign(map[string]interface{}{
		"expires_at": expiresAt,
		"created_at": nowMs,
	}).FirstOrCreate(&row).Error
}

func (r *AuthRepository) GetIssuedTokenExpiry(hash string) (int64, error) {
	var row model.IssuedVerificationToken
	var err error
	for i := 0; i < 5; i++ {
		err = r.db.Where("verification_token_hash = ?", hash).First(&row).Error
		if !isLockError(err) {
			break
		}
		time.Sleep(time.Duration((i+1)*3) * time.Millisecond)
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return 0, ErrNotFound
	}
	if err != nil {
		return 0, err
	}
	return row.ExpiresAt, nil
}

func (r *AuthRepository) VerifyWithTransaction(tokenHash string, nowMs int64) (model.User, error) {
	var outUser model.User
	var err error
	for i := 0; i < 20; i++ {
		err = r.db.Transaction(func(tx *gorm.DB) error {
			var p model.PendingRegistration
			err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("verification_token_hash = ?", tokenHash).First(&p).Error
			if errors.Is(err, gorm.ErrRecordNotFound) {
				var consumed model.ConsumedVerificationToken
				err2 := tx.Where("verification_token_hash = ?", tokenHash).First(&consumed).Error
				if err2 == nil {
					return ErrConsumed
				}
				if errors.Is(err2, gorm.ErrRecordNotFound) {
					return ErrNotFound
				}
				return err2
			}
			if err != nil {
				return err
			}
			if p.VerificationExpiresAt < nowMs {
				return ErrExpired
			}

			user := model.User{
				ID:           p.UserID,
				Username:     p.Username,
				Email:        p.Email,
				PasswordHash: p.PasswordHash,
				DisplayName:  p.DisplayName,
				Subject:      p.Subject,
				School:       p.School,
				Role:         p.Role,
				CreatedAt:    nowMs,
				UpdatedAt:    nowMs,
			}
			if err := tx.Create(&user).Error; err != nil {
				return err
			}
			consumed := model.ConsumedVerificationToken{
				VerificationTokenHash: tokenHash,
				UserID:                p.UserID,
				ConsumedAt:            nowMs,
			}
			if err := tx.Create(&consumed).Error; err != nil {
				return err
			}
			if err := tx.Where("user_id = ?", p.UserID).Delete(&model.PendingRegistration{}).Error; err != nil {
				return err
			}
			outUser = user
			return nil
		})
		if isLockError(err) {
			time.Sleep(time.Duration((i+1)*5) * time.Millisecond)
			continue
		}
		break
	}
	if errors.Is(err, gorm.ErrDuplicatedKey) || isDuplicateConstraintError(err) {
		return model.User{}, ErrConsumed
	}
	return outUser, err
}

var ErrConsumed = errors.New("verification token consumed")
var ErrExpired = errors.New("verification token expired")

func isDuplicateConstraintError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "duplicate key") || strings.Contains(msg, "unique constraint failed")
}

func isLockError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "database is locked") || strings.Contains(msg, "database table is locked")
}
