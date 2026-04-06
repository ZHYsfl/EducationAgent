package util

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// APIResponse 统一响应结构（code/message/data）
type APIResponse struct {
	Code    int    `json:"code"`
	Message string `json:"message,omitempty"`
	Data    any    `json:"data,omitempty"`
}

// 业务错误码常量
const (
	CodeOK                       = 200
	CodeParamError               = 40001
	CodeParamError2              = 40002 // items 相关参数错误
	CodeUnauthorized             = 40100
	CodeNotFound                   = 40400
	CodeConflict                   = 40900
	CodeInternalError              = 50000
	CodeDependencyUnavailable     = 50200
)

// OK 返回成功响应
func OK(c *gin.Context, data any) {
	c.JSON(http.StatusOK, APIResponse{
		Code:    CodeOK,
		Message: "success",
		Data:    data,
	})
}

// Fail 返回失败响应
func Fail(c *gin.Context, code int, message string) {
	c.JSON(http.StatusOK, APIResponse{
		Code:    code,
		Message: message,
		Data:    nil,
	})
}
