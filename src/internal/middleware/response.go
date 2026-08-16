package middleware

import (
	"net/http"

	"educationagent/internal/model"

	"github.com/gin-gonic/gin"
)

// OK writes a successful UniformResponse with data.
func OK[T any](c *gin.Context, data T) {
	c.JSON(http.StatusOK, model.UniformResponse[T]{
		Code:    200,
		Message: "success",
		Data:    data,
	})
}

// Fail writes a UniformResponse with the supplied HTTP status and message.
func Fail(c *gin.Context, code int, message string) {
	c.JSON(code, model.UniformResponse[any]{
		Code:    code,
		Message: message,
		Data:    nil,
	})
}
