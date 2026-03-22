package handler

import (
	"strconv"

	"github.com/gin-gonic/gin"
)

// parsePagination 解析 ?page=1&page_size=20 分页参数
func parsePagination(c *gin.Context) (page, pageSize int) {
	page = 1
	pageSize = 20
	if p := c.Query("page"); p != "" {
		if v, err := strconv.Atoi(p); err == nil && v > 0 {
			page = v
		}
	}
	if ps := c.Query("page_size"); ps != "" {
		if v, err := strconv.Atoi(ps); err == nil && v > 0 && v <= 100 {
			pageSize = v
		}
	}
	return
}
