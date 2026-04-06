package http

import (
	"github.com/gin-gonic/gin"

	"zcxppt/internal/http/handlers"
	"zcxppt/internal/http/middleware"
)

func NewRouter(
	pptHandler *handlers.PPTHandler,
	feedbackHandler *handlers.FeedbackHandler,
	exportHandler *handlers.ExportHandler,
	authMiddleware *middleware.AuthMiddleware,
) *gin.Engine {
	r := gin.Default()
	v1 := r.Group("/api/v1")
	{
		v1.POST("/ppt/init", authMiddleware.RequireInternalKey(), pptHandler.Init)
		v1.POST("/ppt/feedback", authMiddleware.RequireInternalKey(), feedbackHandler.Feedback)
		v1.GET("/canvas/status", authMiddleware.RequireInternalKey(), pptHandler.CanvasStatus)
		v1.POST("/canvas/vad-event", authMiddleware.RequireInternalKey(), pptHandler.VADEvent)
	}

	internal := r.Group("/internal")
	{
		internal.POST("/feedback/generate_pages", authMiddleware.RequireInternalKey(), feedbackHandler.GeneratePages)
		internal.POST("/feedback/timeout_tick", authMiddleware.RequireInternalKey(), feedbackHandler.TickTimeout)
		internal.POST("/ppt/export", authMiddleware.RequireInternalKey(), exportHandler.Create)
		internal.GET("/ppt/export/:export_id", authMiddleware.RequireInternalKey(), exportHandler.Get)
		internal.GET("/ppt/page/:page_id/render", authMiddleware.RequireInternalKey(), pptHandler.PageRender)
	}

	// Health check (no auth required)
	r.GET("/health", func(c *gin.Context) {
		c.JSON(200, gin.H{"code": 200, "message": "ok", "service": "zcxppt"})
	})
	r.GET("/ready", func(c *gin.Context) {
		c.JSON(200, gin.H{"code": 200, "message": "ready"})
	})

	return r
}
