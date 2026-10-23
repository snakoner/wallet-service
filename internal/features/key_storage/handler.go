package key_storage

import (
	"context"

	"github.com/gin-gonic/gin"

	"gitlab.tve-development.com/backend/gtoken/wallet-service/internal/app/middleware"
	"gitlab.tve-development.com/backend/gtoken/wallet-service/internal/constants"
	"gitlab.tve-development.com/backend/gtoken/wallet-service/internal/infra/httputil"
	"gitlab.tve-development.com/backend/gtoken/wallet-service/internal/infra/logger"
)

type KeyStorageService interface {
	GetRoleKey(ctx context.Context, params GetRoleKeyParams) (*WalletKey, error)
	GetRoleKeys(ctx context.Context, params GetRoleKeyParams) ([]WalletKey, error)
}

type Handler struct {
	service KeyStorageService
	log     *logger.Logger
}

func NewHandler(service KeyStorageService, log *logger.Logger) *Handler {
	return &Handler{service: service, log: log}
}

// GetRoleKey godoc
// @Summary     Get active key of own single role
// @Description Returns current key of role, e.g. gas station. Key changes after rotation, so do not store key id in config.
// @Tags        keys
// @Produce     json
// @Param       purpose path  string true  "Role purpose, e.g. gas_station"
// @Param       label   query string false "Purpose refinement, e.g. atoken, usdt_high_risk"
// @Param       network query string true  "Network: ethereum, tron, arbitrum"
// @Success     200 {object} httputil.Response{data=key_storage.KeyResponse}
// @Failure     400 {object} httputil.ErrorResponse
// @Failure     401 {object} httputil.ErrorResponse
// @Failure     404 {object} httputil.ErrorResponse
// @Security    ServiceKey
// @Router      /api/v1/roles/{purpose}/key [get]
func (h *Handler) GetRoleKey(c *gin.Context) {
	service, err := middleware.GetService(c)
	if err != nil {
		httputil.Fail(c, err)
		return
	}

	var query GetRoleKeyQuery
	if err := c.ShouldBindQuery(&query); err != nil {
		httputil.BadRequestValidation(c, err)
		return
	}

	network, _ := constants.ParseNetwork(query.Network)

	key, err := h.service.GetRoleKey(c.Request.Context(), GetRoleKeyParams{
		Service: service,
		Purpose: c.Param("purpose"),
		Label:   query.Label,
		Network: network,
	})
	if err != nil {
		h.log.Error().Err(err).Str("purpose", c.Param("purpose")).Msg("get role key")
		httputil.Fail(c, err)
		return
	}

	httputil.OK(c, NewKeyResponse(key))
}

// GetRoleKeys godoc
// @Summary     List keys of own role
// @Description Returns all keys of single or pool role with statuses, e.g. executors. Sign only with active keys, rotating keys finish sent txs.
// @Tags        keys
// @Produce     json
// @Param       purpose path  string true  "Role purpose, e.g. executor"
// @Param       label   query string false "Purpose refinement"
// @Param       network query string true  "Network: ethereum, tron, arbitrum"
// @Success     200 {object} httputil.Response{data=[]key_storage.KeyResponse}
// @Failure     400 {object} httputil.ErrorResponse
// @Failure     401 {object} httputil.ErrorResponse
// @Failure     404 {object} httputil.ErrorResponse
// @Security    ServiceKey
// @Router      /api/v1/roles/{purpose}/keys [get]
func (h *Handler) GetRoleKeys(c *gin.Context) {
	service, err := middleware.GetService(c)
	if err != nil {
		httputil.Fail(c, err)
		return
	}

	var query GetRoleKeyQuery
	if err := c.ShouldBindQuery(&query); err != nil {
		httputil.BadRequestValidation(c, err)
		return
	}

	network, _ := constants.ParseNetwork(query.Network)

	keys, err := h.service.GetRoleKeys(c.Request.Context(), GetRoleKeyParams{
		Service: service,
		Purpose: c.Param("purpose"),
		Label:   query.Label,
		Network: network,
	})
	if err != nil {
		h.log.Error().Err(err).Str("purpose", c.Param("purpose")).Msg("get role keys")
		httputil.Fail(c, err)
		return
	}

	httputil.OK(c, NewKeysResponse(keys))
}
