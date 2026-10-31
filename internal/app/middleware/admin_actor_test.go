package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	adminaudit "gitlab.tve-development.com/backend/gtoken/wallet-service/internal/features/admin_audit"
)

func TestAdminActor(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for header, want := range map[string]string{"john": "john", "": "unknown"} {
		var name, source string
		r := gin.New()
		r.GET("/", AdminActor(), func(c *gin.Context) {
			name, source, _ = adminaudit.ActorFrom(c.Request.Context())
		})
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		if header != "" {
			req.Header.Set(adminActorHeader, header)
		}
		r.ServeHTTP(httptest.NewRecorder(), req)

		if name != want || source != adminaudit.SourceAPI {
			t.Fatalf("header %q: actor = %q, source = %q", header, name, source)
		}
	}
}
