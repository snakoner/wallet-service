package service_client

import (
	"context"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"gitlab.tve-development.com/backend/gtoken/wallet-service/internal/infra/httputil"
	"gitlab.tve-development.com/backend/gtoken/wallet-service/internal/infra/logger"
)

type AdminService interface {
	CreateClient(ctx context.Context, service string) (string, *ServiceClient, error)
	RotateClient(ctx context.Context, service string, grace time.Duration) (string, *ServiceClient, *ServiceClient, error)
	RevokeClient(ctx context.Context, id uuid.UUID) error
	ListClients(ctx context.Context, service string) ([]ServiceClient, error)
}

type AdminHandler struct {
	service AdminService
	log     *logger.Logger
}

func NewAdminHandler(service AdminService, log *logger.Logger) *AdminHandler {
	return &AdminHandler{service: service, log: log}
}

// CreateClient godoc
// @Summary     Register service and issue its X-Service-Key
// @Description Key is shown once, only its hash is stored. Service with active key must be rotated instead.
// @Tags        admin
// @Accept      json
// @Produce     json
// @Param       request body service_client.CreateClientRequest true "Service"
// @Success     200 {object} httputil.Response{data=service_client.IssuedKeyResponse}
// @Failure     400 {object} httputil.ErrorResponse
// @Failure     409 {object} httputil.ErrorResponse
// @Router      /api/v1/admin/service-clients [post]
func (h *AdminHandler) CreateClient(c *gin.Context) {
	var req CreateClientRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		httputil.BadRequestValidation(c, err)
		return
	}

	key, client, err := h.service.CreateClient(c.Request.Context(), req.Service)
	if err != nil {
		h.log.Error().Err(err).Str("service", req.Service).Msg("admin create service client")
		httputil.Fail(c, err)
		return
	}

	httputil.OK(c,
		IssuedKeyResponse{
			ServiceKey: key,
			Client:     NewClientResponse(client, time.Now().UTC()),
		})
}

// RotateClient godoc
// @Summary     Issue new X-Service-Key, old one is accepted for grace period
// @Tags        admin
// @Accept      json
// @Produce     json
// @Param       request body service_client.RotateClientRequest true "Service and grace period"
// @Success     200 {object} httputil.Response{data=service_client.IssuedKeyResponse}
// @Failure     400 {object} httputil.ErrorResponse
// @Failure     404 {object} httputil.ErrorResponse
// @Router      /api/v1/admin/service-clients/rotate [post]
func (h *AdminHandler) RotateClient(c *gin.Context) {
	var req RotateClientRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		httputil.BadRequestValidation(c, err)
		return
	}

	grace := defaultRotationGrace
	if req.GraceSeconds != nil {
		grace = time.Duration(*req.GraceSeconds) * time.Second
	}

	key, client, old, err := h.service.RotateClient(c.Request.Context(), req.Service, grace)
	if err != nil {
		h.log.Error().Err(err).Str("service", req.Service).Msg("admin rotate service client")
		httputil.Fail(c, err)
		return
	}

	now := time.Now().UTC()
	oldResponse := NewClientResponse(old, now)
	httputil.OK(c,
		IssuedKeyResponse{
			ServiceKey: key,
			Client:     NewClientResponse(client, now),
			OldClient:  &oldResponse},
	)
}

// RevokeClient godoc
// @Summary     Reject service key at once
// @Tags        admin
// @Produce     json
// @Param       id path string true "Service client id"
// @Success     200 {object} httputil.Response
// @Failure     404 {object} httputil.ErrorResponse
// @Router      /api/v1/admin/service-clients/{id}/revoke [post]
func (h *AdminHandler) RevokeClient(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		httputil.Fail(c, ErrServiceClientNotFound)
		return
	}

	if err := h.service.RevokeClient(c.Request.Context(), id); err != nil {
		h.log.Error().Err(err).Str("service_client_id", id.String()).Msg("admin revoke service client")
		httputil.Fail(c, err)
		return
	}

	httputil.OK(c, nil)
}

// ListClients godoc
// @Summary     List service keys without hashes
// @Tags        admin
// @Produce     json
// @Param       service query string false "Service name, all services when empty"
// @Success     200 {object} httputil.Response{data=[]service_client.ClientResponse}
// @Router      /api/v1/admin/service-clients [get]
func (h *AdminHandler) ListClients(c *gin.Context) {
	var query ListClientsQuery
	if err := c.ShouldBindQuery(&query); err != nil {
		httputil.BadRequestValidation(c, err)
		return
	}

	clients, err := h.service.ListClients(c.Request.Context(), query.Service)
	if err != nil {
		h.log.Error().Err(err).Msg("admin list service clients")
		httputil.Fail(c, err)
		return
	}

	now := time.Now().UTC()
	resp := make([]ClientResponse, len(clients))
	for i := range clients {
		resp[i] = NewClientResponse(&clients[i], now)
	}

	httputil.OK(c, resp)
}
