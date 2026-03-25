package http

import (
	"github.com/gin-gonic/gin"

	"zcxppt/internal/http/handlers"
	"zcxppt/internal/http/middleware"
)

func NewRouter(
	taskHandler *handlers.TaskHandler,
	pptHandler *handlers.PPTHandler,
	feedbackHandler *handlers.FeedbackHandler,
	exportHandler *handlers.ExportHandler,
	authMiddleware *middleware.AuthMiddleware,
) *gin.Engine {
	r := gin.Default()
	v1 := r.Group("/api/v1")

	tasks := v1.Group("/tasks")
	{
		tasks.POST("", authMiddleware.RequireInternalKey(), taskHandler.Create)
		tasks.GET("/:task_id", authMiddleware.RequireInternalKey(), taskHandler.Get)
		tasks.PUT("/:task_id/status", authMiddleware.RequireInternalKey(), taskHandler.UpdateStatus)
		tasks.GET("", authMiddleware.RequireInternalKey(), taskHandler.List)
		tasks.GET("/:task_id/preview", authMiddleware.RequireInternalKey(), pptHandler.CanvasStatus)
	}

	ppt := v1.Group("/ppt")
	{
		ppt.POST("/init", authMiddleware.RequireInternalKey(), pptHandler.Init)
		ppt.POST("/feedback", authMiddleware.RequireInternalKey(), feedbackHandler.Feedback)
		ppt.POST("/export", authMiddleware.RequireInternalKey(), exportHandler.Create)
		ppt.GET("/export/:export_id", authMiddleware.RequireInternalKey(), exportHandler.Get)
		ppt.GET("/page/:page_id/render", authMiddleware.RequireInternalKey(), pptHandler.PageRender)
	}

	v1.GET("/canvas/status", authMiddleware.RequireInternalKey(), pptHandler.CanvasStatus)
	v1.POST("/internal/feedback/timeout_tick", authMiddleware.RequireInternalKey(), feedbackHandler.TickTimeout)

	return r
}
