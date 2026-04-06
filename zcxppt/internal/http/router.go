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

	ppt := v1.Group("/ppt")
	{
		ppt.POST("/init", authMiddleware.RequireInternalKey(), pptHandler.Init)
		ppt.POST("/generate_pages", authMiddleware.RequireInternalKey(), feedbackHandler.GeneratePages)
		ppt.POST("/feedback", authMiddleware.RequireInternalKey(), feedbackHandler.Feedback)
		ppt.POST("/export", authMiddleware.RequireInternalKey(), exportHandler.Create)
		ppt.GET("/export/:export_id", authMiddleware.RequireInternalKey(), exportHandler.Get)
		ppt.GET("/page/:page_id/render", authMiddleware.RequireInternalKey(), pptHandler.PageRender)
	}

	v1.GET("/canvas/status", authMiddleware.RequireInternalKey(), pptHandler.CanvasStatus)
	v1.POST("/canvas/vad-event", authMiddleware.RequireInternalKey(), pptHandler.VADEvent)
	v1.POST("/internal/feedback/timeout_tick", authMiddleware.RequireInternalKey(), feedbackHandler.TickTimeout)

	// Health check (no auth required)
	r.GET("/health", func(c *gin.Context) {
		c.JSON(200, gin.H{"code": 200, "message": "ok", "service": "zcxppt"})
	})
	r.GET("/ready", func(c *gin.Context) {
		c.JSON(200, gin.H{"code": 200, "message": "ready"})
	})

	return r
}
