package contract

import "github.com/gin-gonic/gin"

type APIResponse struct {
	Code    int         `json:"code"`
	Message string      `json:"message,omitempty"`
	Data    interface{} `json:"data,omitempty"`
}

func Success(c *gin.Context, data interface{}, message string) {
	resp := APIResponse{Code: CodeSuccess, Data: data}
	if message != "" {
		resp.Message = message
	}
	c.JSON(200, resp)
}

func Error(c *gin.Context, code int, message string) {
	c.JSON(200, APIResponse{Code: code, Message: message, Data: nil})
}
