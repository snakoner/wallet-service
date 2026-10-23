package middleware

import (
	"context"
	"errors"

	"github.com/gin-gonic/gin"

	serviceclient "gitlab.tve-development.com/backend/gtoken/wallet-service/internal/features/service_client"
	"gitlab.tve-development.com/backend/gtoken/wallet-service/internal/infra/httputil"
	"gitlab.tve-development.com/backend/gtoken/wallet-service/internal/infra/logger"
)

const (
	serviceKeyHeader  = "X-Service-Key"
	ContextServiceKey = "service"
)

type authenticator interface {
	Authenticate(ctx context.Context, key string) (*serviceclient.ServiceClient, error)
}

type ServiceAuth struct {
	auth authenticator
	log  *logger.Logger
}

func NewServiceAuth(auth authenticator, log *logger.Logger) *ServiceAuth {
	return &ServiceAuth{auth: auth, log: log}
}

func (m *ServiceAuth) Handle() gin.HandlerFunc {
	return func(c *gin.Context) {
		key := c.GetHeader(serviceKeyHeader)
		c.Request.Header.Del(serviceKeyHeader)

		client, err := m.auth.Authenticate(c.Request.Context(), key)
		if err != nil {
			if !errors.Is(err, serviceclient.ErrUnauthorized) {
				m.log.Error().Err(err).Msg("authenticate service")
			}

			httputil.Fail(c, err)
			c.Abort()
			return
		}

		c.Set(ContextServiceKey, client.Service)
		c.Next()
	}
}

func GetService(c *gin.Context) (string, error) {
	service := c.GetString(ContextServiceKey)
	if service == "" {
		return "", serviceclient.ErrUnauthorized
	}

	return service, nil
}
