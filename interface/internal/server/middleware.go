package server

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

func AuthUserMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		userID := parseUserID(c.GetHeader("Authorization"))
		if userID == "" {
			c.JSON(http.StatusOK, APIResponse{Code: 40100, Message: "未授权或 token 非法", Data: nil})
			c.Abort()
			return
		}
		c.Set("user_id", userID)
		c.Next()
	}
}

func userIDFromContext(c *gin.Context) string {
	if v, ok := c.Get("user_id"); ok {
		if s, ok2 := v.(string); ok2 && strings.HasPrefix(s, "user_") {
			return s
		}
	}
	return ""
}
