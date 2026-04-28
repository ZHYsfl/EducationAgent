package handler

import (
	"github.com/gin-gonic/gin"
	"educationagent/internal/middleware"
	"educationagent/internal/model"
	"educationagent/internal/service"
)

// KBQueryChunks handles POST /api/v1/kb/query-chunks
func KBQueryChunks(kbSvc service.KBService) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req model.QueryChunksRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			middleware.Fail(c, 400, "invalid request body")
			return
		}

		chunks, total, err := kbSvc.QueryChunks(c.Request.Context(), req.Query)
		if err != nil {
			middleware.Fail(c, 400, "failed to query the chunks from the kb service")
			return
		}

		middleware.OK(c, model.QueryChunksData{
			Chunks: chunks,
			Total:  total,
		})
	}
}
