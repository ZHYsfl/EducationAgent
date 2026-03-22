package http

import (
	"github.com/gin-gonic/gin"

	"auth_memory_service/internal/http/handlers"
	"auth_memory_service/internal/http/middleware"
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

func AddMemoryRoutes(r *gin.Engine, memoryHandler *handlers.MemoryHandler, authMiddleware *middleware.AuthMiddleware) {
	v1 := r.Group("/api/v1")
	memory := v1.Group("/memory")
	{
		memory.POST("/extract", authMiddleware.RequireInternalKey(), memoryHandler.Extract)
		memory.POST("/recall", authMiddleware.RequireInternalKey(), memoryHandler.Recall)
		memory.GET("/profile/:user_id", authMiddleware.RequireMemoryProfileAccess(), memoryHandler.GetProfile)
		memory.PUT("/profile/:user_id", authMiddleware.RequireMemoryProfileAccess(), memoryHandler.UpdateProfile)
		memory.POST("/working/save", authMiddleware.RequireInternalKey(), memoryHandler.SaveWorking)
		memory.GET("/working/:session_id", authMiddleware.RequireInternalKey(), memoryHandler.GetWorking)
	}
}
