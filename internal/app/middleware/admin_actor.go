package middleware

import (
	"github.com/gin-gonic/gin"

	adminaudit "gitlab.tve-development.com/backend/gtoken/wallet-service/internal/features/admin_audit"
)

const adminActorHeader = "X-Admin-Actor"

func AdminActor() gin.HandlerFunc {
	return func(c *gin.Context) {
		actor := c.GetHeader(adminActorHeader)
		if actor == "" {
			actor = "unknown"
		}

		c.Request = c.Request.WithContext(adminaudit.WithActor(c.Request.Context(), actor, adminaudit.SourceAPI))
		c.Next()
	}
}
