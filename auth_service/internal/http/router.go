package http

import (
	"github.com/gin-gonic/gin"

	"auth_service/internal/http/handlers"
	"auth_service/internal/http/middleware"
)

func NewRouter(authHandler *handlers.AuthHandler, authMiddleware *middleware.AuthMiddleware) *gin.Engine {
	r := gin.Default()
	v1 := r.Group("/api/v1")
	auth := v1.Group("/auth")
	{
		auth.POST("/register", authHandler.Register)
		auth.POST("/verify", authHandler.Verify)
		auth.POST("/login", authHandler.Login)
		auth.GET("/profile", authMiddleware.RequireBearer(), authHandler.Profile)
	}
	return r
}
