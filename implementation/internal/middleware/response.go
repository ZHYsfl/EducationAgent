package middleware

import (
	"net/http"

	"educationagent/internal/model"

	"github.com/gin-gonic/gin"
)

// JSON writes a uniform response.
func JSON(c *gin.Context, code int, message string, data any) {
	c.JSON(http.StatusOK, model.UniformResponse{
		Code:    code,
		Message: message,
		Data:    data,
	})
}

// OK is a convenience helper for success responses.
func OK(c *gin.Context, data any) {
	JSON(c, 200, "success", data)
}

// Fail is a convenience helper for failure responses.
func Fail(c *gin.Context, code int, message string) {
	JSON(c, code, message, nil)
}
