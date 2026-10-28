package httputil

import (
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestParsePagination(t *testing.T) {
	gin.SetMode(gin.TestMode)

	for _, tc := range []struct {
		query         string
		offset, limit int
	}{
		{"", 0, 100},
		{"?offset=20&limit=50", 20, 50},
		{"?limit=1000", 0, 1000},
		{"?limit=1001", 0, 1000},
		{"?offset=-1&limit=-1", 0, 100},
		{"?limit=0", 0, 100},
		{"?offset=abc&limit=1.5", 0, 100},
		{"?offset=0", 0, 100},
	} {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest("GET", "/"+tc.query, nil)

		offset, limit := ParsePagination(c)
		if offset != tc.offset || limit != tc.limit {
			t.Errorf("%q: offset, limit = %d, %d, want %d, %d", tc.query, offset, limit, tc.offset, tc.limit)
		}
	}
}
