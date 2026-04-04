package middleware

import (
	"strings"

	"github.com/gin-gonic/gin"

	"zcxppt/internal/contract"
)

// AuthMiddleware delegates auth trust to unified gateway/middleware layer.
// It only validates required headers and propagates user context.
type AuthMiddleware struct {
	internalKey string
}

func NewAuthMiddleware(_ string, internalKey string) *AuthMiddleware {
	return &AuthMiddleware{internalKey: internalKey}
}

func (m *AuthMiddleware) RequireBearer() gin.HandlerFunc {
	return func(c *gin.Context) {
		auth := c.GetHeader("Authorization")
		if auth == "" || !strings.HasPrefix(auth, "Bearer ") {
			contract.Error(c, contract.CodeInvalidCredentials, "invalid credentials")
			c.Abort()
			return
		}
		if userID := strings.TrimSpace(c.GetHeader("X-User-ID")); userID != "" {
			c.Set("user_id", userID)
		}
		c.Next()
	}
}

func (m *AuthMiddleware) RequireInternalKey() gin.HandlerFunc {
	return func(c *gin.Context) {
		if m.internalKey == "" || c.GetHeader("X-Internal-Key") != m.internalKey {
			contract.Error(c, contract.CodeInvalidCredentials, "invalid credentials")
			c.Abort()
			return
		}
		c.Next()
	}
}
