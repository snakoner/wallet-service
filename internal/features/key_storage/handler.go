package key_storage

import (
	"context"
	"errors"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"gitlab.tve-development.com/backend/gtoken/wallet-service/internal/app/middleware"
	"gitlab.tve-development.com/backend/gtoken/wallet-service/internal/constants"
	"gitlab.tve-development.com/backend/gtoken/wallet-service/internal/infra/httputil"
	"gitlab.tve-development.com/backend/gtoken/wallet-service/internal/infra/logger"
)

type KeyStorageService interface {
	GetRoleKey(ctx context.Context, params GetRoleKeyParams) (*WalletKey, error)
	GetRoleKeys(ctx context.Context, params GetRoleKeyParams) ([]WalletKey, error)
	AcquireLease(ctx context.Context, params AcquireLeaseParams) (*Lease, error)
	ExtendLease(ctx context.Context, service string, leaseID uuid.UUID, ttl time.Duration) (time.Time, error)
	ReleaseLease(ctx context.Context, service string, leaseID uuid.UUID) error
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

// AcquireLease godoc
// @Summary     Lease free key of own role
// @Description Key is used exclusively by caller until lease expires or is released. Pool keys sign only with lease.
// @Tags        keys
// @Accept      json
// @Produce     json
// @Param       purpose path string                               true "Role purpose, e.g. executor"
// @Param       request body key_storage.AcquireLeaseRequest true "Lease params"
// @Success     200 {object} httputil.Response{data=key_storage.LeaseResponse}
// @Failure     400 {object} httputil.ErrorResponse
// @Failure     401 {object} httputil.ErrorResponse
// @Failure     404 {object} httputil.ErrorResponse
// @Failure     409 {object} httputil.ErrorResponse "NO_FREE_KEY, retry after Retry-After seconds"
// @Security    ServiceKey
// @Router      /api/v1/roles/{purpose}/lease [post]
func (h *Handler) AcquireLease(c *gin.Context) {
	service, err := middleware.GetService(c)
	if err != nil {
		httputil.Fail(c, err)
		return
	}

	var req AcquireLeaseRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		httputil.BadRequestValidation(c, err)
		return
	}

	network, _ := constants.ParseNetwork(req.Network)

	lease, err := h.service.AcquireLease(c.Request.Context(), AcquireLeaseParams{
		Service: service,
		Purpose: c.Param("purpose"),
		Label:   req.Label,
		Network: network,
		Owner:   req.Owner,
		TTL:     time.Duration(req.TTLSeconds) * time.Second,
		Exclude: req.Exclude,
	})
	if err != nil {
		if errors.Is(err, ErrNoFreeKey) {
			c.Header("Retry-After", "1")
		} else {
			h.log.Error().Err(err).Str("purpose", c.Param("purpose")).Msg("acquire lease")
		}

		httputil.Fail(c, err)
		return
	}

	httputil.OK(c, LeaseResponse{LeaseID: lease.ID, ExpiresAt: lease.ExpiresAt, Key: NewKeyResponse(&lease.Key)})
}

// ExtendLease godoc
// @Summary     Extend own lease
// @Tags        keys
// @Accept      json
// @Produce     json
// @Param       id      path string                              true  "Lease id"
// @Param       request body key_storage.ExtendLeaseRequest false "TTL"
// @Success     200 {object} httputil.Response{data=key_storage.ExtendLeaseResponse}
// @Failure     401 {object} httputil.ErrorResponse
// @Failure     409 {object} httputil.ErrorResponse
// @Security    ServiceKey
// @Router      /api/v1/leases/{id}/extend [post]
func (h *Handler) ExtendLease(c *gin.Context) {
	service, leaseID, ok := h.leaseParams(c)
	if !ok {
		return
	}

	var req ExtendLeaseRequest
	if c.Request.ContentLength != 0 {
		if err := c.ShouldBindJSON(&req); err != nil {
			httputil.BadRequestValidation(c, err)
			return
		}
	}

	expiresAt, err := h.service.ExtendLease(c.Request.Context(), service, leaseID, time.Duration(req.TTLSeconds)*time.Second)
	if err != nil {
		httputil.Fail(c, err)
		return
	}

	httputil.OK(c, ExtendLeaseResponse{ExpiresAt: expiresAt})
}

// ReleaseLease godoc
// @Summary     Release own lease
// @Tags        keys
// @Produce     json
// @Param       id path string true "Lease id"
// @Success     200 {object} httputil.Response
// @Failure     401 {object} httputil.ErrorResponse
// @Failure     409 {object} httputil.ErrorResponse
// @Security    ServiceKey
// @Router      /api/v1/leases/{id} [delete]
func (h *Handler) ReleaseLease(c *gin.Context) {
	service, leaseID, ok := h.leaseParams(c)
	if !ok {
		return
	}

	if err := h.service.ReleaseLease(c.Request.Context(), service, leaseID); err != nil {
		httputil.Fail(c, err)
		return
	}

	httputil.OK(c, nil)
}

func (h *Handler) leaseParams(c *gin.Context) (string, uuid.UUID, bool) {
	service, err := middleware.GetService(c)
	if err != nil {
		httputil.Fail(c, err)
		return "", uuid.Nil, false
	}

	leaseID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		httputil.Fail(c, ErrLeaseExpired)
		return "", uuid.Nil, false
	}

	return service, leaseID, true
}
