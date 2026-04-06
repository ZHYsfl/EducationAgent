package repository

import (
	"errors"

	"gorm.io/gorm"

	"memory_service/internal/model"
)

var ErrNotFound = errors.New("not found")

type AuthRepository struct {
	db *gorm.DB
}

func NewAuthRepository(db *gorm.DB) *AuthRepository {
	return &AuthRepository{db: db}
}

func (r *AuthRepository) GetUserByID(id string) (model.User, error) {
	var user model.User
	err := r.db.Where("id = ?", id).First(&user).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return model.User{}, ErrNotFound
	}
	return user, err
}

func (r *AuthRepository) GetSessionByID(id string) (model.Session, error) {
	var session model.Session
	err := r.db.Where("id = ?", id).First(&session).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return model.Session{}, ErrNotFound
	}
	return session, err
}
