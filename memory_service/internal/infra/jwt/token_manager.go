package jwtinfra

import (
	"errors"
	"fmt"

	"github.com/golang-jwt/jwt/v5"

	"memory_service/internal/util"
)

var ErrTokenInvalid = errors.New("token invalid")
var ErrTokenExpired = errors.New("token expired")

type TokenManager struct {
	secret []byte
	ttlMs  int64
}

func NewTokenManager(secret string, ttlHours int) *TokenManager {
	return &TokenManager{secret: []byte(secret), ttlMs: int64(ttlHours) * 60 * 60 * 1000}
}

func (m *TokenManager) Generate(userID, username string) (string, int64, error) {
	expiresAt := util.NowMilli() + m.ttlMs
	claims := jwt.MapClaims{
		"user_id":  userID,
		"username": username,
		"exp":      expiresAt,
	}
	t := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	token, err := t.SignedString(m.secret)
	if err != nil {
		return "", 0, err
	}
	return token, expiresAt, nil
}

func (m *TokenManager) Parse(token string) (userID string, username string, exp int64, err error) {
	parsed, err := jwt.Parse(token, func(t *jwt.Token) (interface{}, error) {
		if t.Method != jwt.SigningMethodHS256 {
			return nil, fmt.Errorf("unexpected signing method")
		}
		return m.secret, nil
	})
	if err != nil {
		return "", "", 0, ErrTokenInvalid
	}
	if !parsed.Valid {
		return "", "", 0, ErrTokenInvalid
	}
	claims, ok := parsed.Claims.(jwt.MapClaims)
	if !ok {
		return "", "", 0, ErrTokenInvalid
	}
	uid, ok := claims["user_id"].(string)
	if !ok || uid == "" {
		return "", "", 0, ErrTokenInvalid
	}
	uname, ok := claims["username"].(string)
	if !ok || uname == "" {
		return "", "", 0, ErrTokenInvalid
	}
	expFloat, ok := claims["exp"].(float64)
	if !ok {
		return "", "", 0, ErrTokenInvalid
	}
	expiresAt := int64(expFloat)
	if util.NowMilli() > expiresAt {
		return "", "", 0, ErrTokenExpired
	}
	return uid, uname, expiresAt, nil
}
