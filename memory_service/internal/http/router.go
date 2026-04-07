package http

import (
	"github.com/gin-gonic/gin"

	"memory_service/internal/http/handlers"
	"memory_service/internal/http/middleware"
)

func NewRouter(memoryHandler *handlers.MemoryHandler, authMiddleware *middleware.AuthMiddleware) *gin.Engine {
	r := gin.Default()
	api := r.Group("/api", authMiddleware.RequestMetadata())
	v1 := api.Group("/v1")
	memory := v1.Group("/memory", authMiddleware.RequireInternalService())
	{
		memory.POST("/recall", memoryHandler.AcceptRecall)
		memory.POST("/context/push", memoryHandler.ContextPush)
	}

	compatibility := v1.Group("/memory")
	{
		compatibility.POST("/extract", authMiddleware.MarkDeprecated("compatibility"), authMiddleware.RequireInternalService(), memoryHandler.Extract)
		compatibility.GET("/profile/:user_id", authMiddleware.MarkDeprecated("compatibility"), authMiddleware.RequireLegacyProfileAccess("user_id"), memoryHandler.GetProfile)
		compatibility.PUT("/profile/:user_id", authMiddleware.MarkDeprecated("compatibility"), authMiddleware.RequireLegacyProfileAccess("user_id"), memoryHandler.UpdateProfile)
		compatibility.POST("/working/save", authMiddleware.MarkDeprecated("compatibility"), authMiddleware.RequireInternalService(), memoryHandler.SaveWorking)
		compatibility.GET("/working/:session_id", authMiddleware.MarkDeprecated("compatibility"), authMiddleware.RequireInternalService(), memoryHandler.GetWorking)
	}

	internal := api.Group("/internal/memory", authMiddleware.RequireInternalService())
	{
		// /recall/sync exists for smoke tests, deterministic verification, and
		// callback generation support only. Do not use it as a new integration surface.
		internal.POST("/recall/sync", memoryHandler.RecallSync)
		internal.POST("/extract", memoryHandler.Extract)
		internal.GET("/profile/:user_id", memoryHandler.GetProfile)
		internal.PUT("/profile/:user_id", memoryHandler.UpdateProfile)
		internal.POST("/working/save", memoryHandler.SaveWorking)
		internal.GET("/working/:session_id", memoryHandler.GetWorking)
	}
	return r
}
