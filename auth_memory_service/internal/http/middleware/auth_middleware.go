package middleware

import (
	"strings"

	"github.com/gin-gonic/gin"

	"auth_memory_service/internal/contract"
	jwtinfra "auth_memory_service/internal/infra/jwt"
)

type AuthMiddleware struct {
	tokenManager *jwtinfra.TokenManager
	internalKey  string
}

func NewAuthMiddleware(tokenManager *jwtinfra.TokenManager, internalKey string) *AuthMiddleware {
	return &AuthMiddleware{tokenManager: tokenManager, internalKey: internalKey}
}

func (m *AuthMiddleware) RequireBearer() gin.HandlerFunc {
	return func(c *gin.Context) {
		auth := c.GetHeader("Authorization")
		if auth == "" || !strings.HasPrefix(auth, "Bearer ") {
			contract.Error(c, contract.CodeInvalidCredentials, "invalid credentials")
			c.Abort()
			return
		}
		token := strings.TrimPrefix(auth, "Bearer ")
		userID, _, _, err := m.tokenManager.Parse(token)
		if err == jwtinfra.ErrTokenExpired {
			contract.Error(c, contract.CodeTokenExpired, "token expired")
			c.Abort()
			return
		}
		if err != nil {
			contract.Error(c, contract.CodeInvalidCredentials, "invalid credentials")
			c.Abort()
			return
		}
		c.Set("user_id", userID)
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

// This dual-mode policy is integration-driven and retained from approved plan.
func (m *AuthMiddleware) RequireMemoryProfileAccess() gin.HandlerFunc {
	return func(c *gin.Context) {
		if m.internalKey != "" && c.GetHeader("X-Internal-Key") == m.internalKey {
			c.Set("auth_mode", "internal")
			c.Next()
			return
		}
		auth := c.GetHeader("Authorization")
		if auth == "" || !strings.HasPrefix(auth, "Bearer ") {
			contract.Error(c, contract.CodeInvalidCredentials, "invalid credentials")
			c.Abort()
			return
		}
		token := strings.TrimPrefix(auth, "Bearer ")
		userID, _, _, err := m.tokenManager.Parse(token)
		if err == jwtinfra.ErrTokenExpired {
			contract.Error(c, contract.CodeTokenExpired, "token expired")
			c.Abort()
			return
		}
		if err != nil {
			contract.Error(c, contract.CodeInvalidCredentials, "invalid credentials")
			c.Abort()
			return
		}
		pathUserID := c.Param("user_id")
		if pathUserID == "" || pathUserID != userID {
			contract.Error(c, contract.CodeInvalidCredentials, "invalid credentials")
			c.Abort()
			return
		}
		c.Set("auth_mode", "bearer")
		c.Set("user_id", userID)
		c.Next()
	}
}
