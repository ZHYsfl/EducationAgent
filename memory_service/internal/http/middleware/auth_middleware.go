package middleware

import (
	"log"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"memory_service/internal/contract"
	jwtinfra "memory_service/internal/infra/jwt"
)

const (
	ContextKeyRequestID   = "request_id"
	ContextKeyAuthMode    = "auth_mode"
	ContextKeyCallerType  = "caller_type"
	ContextKeyActorUserID = "actor_user_id"
	ContextKeyRouteClass  = "route_class"
)

type AuthMiddleware struct {
	tokenManager *jwtinfra.TokenManager
	internalKey  string
}

func NewAuthMiddleware(tokenManager *jwtinfra.TokenManager, internalKey string) *AuthMiddleware {
	return &AuthMiddleware{tokenManager: tokenManager, internalKey: internalKey}
}

func (m *AuthMiddleware) RequestMetadata() gin.HandlerFunc {
	return func(c *gin.Context) {
		requestID := strings.TrimSpace(c.GetHeader("X-Request-ID"))
		if requestID == "" {
			requestID = uuid.NewString()
		}
		c.Set(ContextKeyRequestID, requestID)
		c.Header("X-Request-ID", requestID)
		c.Next()
	}
}

func (m *AuthMiddleware) RequireBearerUser() gin.HandlerFunc {
	return func(c *gin.Context) {
		m.captureAttemptedAuth(c)
		userID, err := m.parseBearerUser(c)
		if err != nil {
			m.abortAuthError(c, err)
			return
		}
		c.Set(ContextKeyActorUserID, userID)
		c.Set(ContextKeyCallerType, "end_user")
		c.Set(ContextKeyAuthMode, "bearer")
		c.Next()
	}
}

func (m *AuthMiddleware) RequirePathUserMatch(pathParam string) gin.HandlerFunc {
	return func(c *gin.Context) {
		actorUserID, ok := c.Get(ContextKeyActorUserID)
		if !ok || strings.TrimSpace(actorUserID.(string)) == "" || strings.TrimSpace(c.Param(pathParam)) != strings.TrimSpace(actorUserID.(string)) {
			contract.Error(c, contract.CodeInvalidCredentials, "invalid credentials")
			c.Abort()
			return
		}
		c.Next()
	}
}

func (m *AuthMiddleware) RequireInternalService() gin.HandlerFunc {
	return func(c *gin.Context) {
		m.captureAttemptedAuth(c)
		if m.internalKey == "" || c.GetHeader("X-Internal-Key") != m.internalKey {
			contract.Error(c, contract.CodeInvalidCredentials, "invalid credentials")
			c.Abort()
			return
		}
		c.Set(ContextKeyCallerType, "internal_service")
		c.Set(ContextKeyAuthMode, "internal")
		c.Next()
	}
}

// Temporary bridge for deprecated profile compatibility routes only.
func (m *AuthMiddleware) RequireLegacyProfileAccess(pathParam string) gin.HandlerFunc {
	return func(c *gin.Context) {
		m.captureAttemptedAuth(c)
		if m.internalKey != "" && c.GetHeader("X-Internal-Key") == m.internalKey {
			c.Set(ContextKeyCallerType, "internal_service")
			c.Set(ContextKeyAuthMode, "internal")
			c.Next()
			return
		}
		userID, err := m.parseBearerUser(c)
		if err != nil {
			m.abortAuthError(c, err)
			return
		}
		if strings.TrimSpace(userID) != strings.TrimSpace(c.Param(pathParam)) {
			contract.Error(c, contract.CodeInvalidCredentials, "invalid credentials")
			c.Abort()
			return
		}
		c.Set(ContextKeyActorUserID, userID)
		c.Set(ContextKeyCallerType, "end_user")
		c.Set(ContextKeyAuthMode, "bearer")
		c.Next()
	}
}

func (m *AuthMiddleware) MarkDeprecated(routeClass string) gin.HandlerFunc {
	return func(c *gin.Context) {
		m.captureAttemptedAuth(c)
		c.Set(ContextKeyRouteClass, routeClass)
		c.Header("Deprecation", "true")
		c.Header("X-API-Status", routeClass)
		c.Next()
		requestID, _ := c.Get(ContextKeyRequestID)
		authMode, _ := c.Get(ContextKeyAuthMode)
		callerType, _ := c.Get(ContextKeyCallerType)
		log.Printf(
			"component=memory_service route_class=%s deprecated=true method=%s path=%s status=%d auth_mode=%v caller_type=%v request_id=%v",
			routeClass,
			c.Request.Method,
			c.FullPath(),
			c.Writer.Status(),
			authMode,
			callerType,
			requestID,
		)
	}
}

func (m *AuthMiddleware) captureAttemptedAuth(c *gin.Context) {
	if _, exists := c.Get(ContextKeyAuthMode); exists {
		return
	}
	if strings.TrimSpace(c.GetHeader("X-Internal-Key")) != "" {
		c.Set(ContextKeyAuthMode, "internal")
		c.Set(ContextKeyCallerType, "internal_service")
		return
	}
	auth := strings.TrimSpace(c.GetHeader("Authorization"))
	if strings.HasPrefix(auth, "Bearer ") {
		c.Set(ContextKeyAuthMode, "bearer")
		c.Set(ContextKeyCallerType, "end_user")
		return
	}
	c.Set(ContextKeyAuthMode, "none")
	c.Set(ContextKeyCallerType, "anonymous")
}

func (m *AuthMiddleware) parseBearerUser(c *gin.Context) (string, error) {
	auth := c.GetHeader("Authorization")
	if auth == "" || !strings.HasPrefix(auth, "Bearer ") {
		return "", jwtinfra.ErrTokenInvalid
	}
	token := strings.TrimPrefix(auth, "Bearer ")
	userID, _, _, err := m.tokenManager.Parse(token)
	if err != nil {
		return "", err
	}
	return userID, nil
}

func (m *AuthMiddleware) abortAuthError(c *gin.Context, err error) {
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
}

func (m *AuthMiddleware) RequireBearer() gin.HandlerFunc {
	return m.RequireBearerUser()
}

func (m *AuthMiddleware) RequireInternalKey() gin.HandlerFunc {
	return m.RequireInternalService()
}

func (m *AuthMiddleware) RequireMemoryProfileAccess() gin.HandlerFunc {
	return m.RequireLegacyProfileAccess("user_id")
}

func RequestID(c *gin.Context) string {
	v, ok := c.Get(ContextKeyRequestID)
	if !ok {
		return ""
	}
	id, ok := v.(string)
	if !ok {
		return ""
	}
	return id
}

func ActorUserID(c *gin.Context) string {
	v, ok := c.Get(ContextKeyActorUserID)
	if !ok {
		return ""
	}
	userID, ok := v.(string)
	if !ok {
		return ""
	}
	return userID
}
