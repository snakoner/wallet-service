package httputil

import (
	"strconv"

	"github.com/gin-gonic/gin"
)

const (
	DefaultOffset int = 0
	DefaultLimit  int = 100
	MaxLimit      int = 1000
)

func ParsePagination(c *gin.Context) (offset int, limit int) {
	offset = DefaultOffset
	limit = DefaultLimit

	if v := c.Query("offset"); v != "" {
		if parsed, err := strconv.Atoi(v); err == nil && parsed >= 0 {
			offset = parsed
		}
	}

	if v := c.Query("limit"); v != "" {
		if parsed, err := strconv.Atoi(v); err == nil && parsed > 0 {
			limit = parsed
		}
	}

	if limit > MaxLimit {
		limit = MaxLimit
	}

	return offset, limit
}
