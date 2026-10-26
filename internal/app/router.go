package app

import (
	"net/http"

	"github.com/gin-gonic/gin"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"

	_ "gitlab.tve-development.com/backend/gtoken/wallet-service/docs"

	"gitlab.tve-development.com/backend/gtoken/wallet-service/internal/app/middleware"
	"gitlab.tve-development.com/backend/gtoken/wallet-service/internal/config"
	"gitlab.tve-development.com/backend/gtoken/wallet-service/internal/constants"
	"gitlab.tve-development.com/backend/gtoken/wallet-service/internal/infra/logger"
)

func NewRouter(
	handlers *Handlers,
	cfg *config.Config,
	log *logger.Logger,
	serviceAuth *middleware.ServiceAuth,
) *gin.Engine {
	r := gin.New()
	r.Use(gin.Recovery())
	r.Use(gin.Logger())

	if constants.Environment(cfg.App.Environment) != constants.EnvironmentProduction {
		r.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))
	}

	r.GET("/healthz", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	api := r.Group("/api/v1")
	{
		// TODO: SSO admin auth; until then admin API is not available in production
		if constants.Environment(cfg.App.Environment) != constants.EnvironmentProduction {
			admin := api.Group("/admin")
			{
				admin.POST("/roles", handlers.Admin.CreateRole)
				admin.GET("/roles", handlers.Admin.ListRoles)
				admin.PUT("/roles/:id/policy", handlers.Admin.UpdatePolicy)
				admin.POST("/roles/:id/keys", handlers.Admin.CreateKey)
				admin.GET("/roles/:id/keys", handlers.Admin.ListRoleKeys)
				admin.GET("/keys", handlers.Admin.ListKeys)
				admin.POST("/keys/:id/rotate", handlers.Admin.RotateKey)
				admin.POST("/keys/:id/activate", handlers.Admin.ActivateKey)
				admin.POST("/keys/:id/revoke", handlers.Admin.RevokeKey)
				admin.POST("/keys/:id/disable", handlers.Admin.DisableKey)
				admin.POST("/keys/:id/enable", handlers.Admin.EnableKey)
			}
		}

		roles := api.Group("/roles", serviceAuth.Handle())
		{
			roles.GET("/:purpose/key", handlers.KeyStorage.GetRoleKey)
			roles.GET("/:purpose/keys", handlers.KeyStorage.GetRoleKeys)
		}

		keys := api.Group(
			"/keys",
			serviceAuth.Handle(),
		)
		{
			keys.POST("/:id/sign-transaction", handlers.Signing.SignTransaction)
		}
	}

	return r
}
