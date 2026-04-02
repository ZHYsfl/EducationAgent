package util

import "github.com/google/uuid"

func NewUserID() string {
	return "user_" + uuid.NewString()
}
