package http

import (
	"github.com/gin-gonic/gin"

	"memory_service/internal/http/handlers"
	"memory_service/internal/http/middleware"
)

func NewRouter(memoryHandler *handlers.MemoryHandler, authMiddleware *middleware.AuthMiddleware) *gin.Engine {
	r := gin.Default()
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
	return r
}
