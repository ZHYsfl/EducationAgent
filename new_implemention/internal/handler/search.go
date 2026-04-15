package handler

import (
	"github.com/gin-gonic/gin"
	"educationagent/internal/middleware"
	"educationagent/internal/model"
	"educationagent/internal/service"
)

// SearchQuery handles POST /api/v1/search/query
func SearchQuery(searchSvc service.SearchService) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req model.SearchQueryRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			middleware.Fail(c, 400, "invalid request body")
			return
		}

		result, err := searchSvc.SearchWeb(c.Request.Context(), req.Query)
		if err != nil {
			middleware.Fail(c, 400, "failed to search the web")
			return
		}

		middleware.OK(c, result)
	}
}
