package key_storage

import (
	"errors"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"gitlab.tve-development.com/backend/gtoken/wallet-service/internal/app/middleware"
	"gitlab.tve-development.com/backend/gtoken/wallet-service/internal/constants"
	"gitlab.tve-development.com/backend/gtoken/wallet-service/internal/infra/httputil"
)

// ReleaseLease godoc
// @Summary     Force release lease of key held by stuck instance
// @Tags        admin
// @Produce     json
// @Param       id path string true "Key id"
// @Success     200 {object} httputil.Response
// @Failure     404 {object} httputil.ErrorResponse
// @Router      /api/v1/admin/keys/{id}/release-lease [post]
func (h *AdminHandler) ReleaseLease(c *gin.Context) {
	keyID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		httputil.Fail(c, ErrKeyNotFound)
		return
	}

	if err := h.service.ForceReleaseLease(c.Request.Context(), keyID); err != nil {
		h.log.Error().
			Err(err).
			Str("key_id", keyID.String()).
			Msg("admin release lease")
		httputil.Fail(c, err)
		return
	}

	httputil.OK(c, nil)
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
