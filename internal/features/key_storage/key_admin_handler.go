package key_storage

import (
	"context"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"gitlab.tve-development.com/backend/gtoken/wallet-service/internal/constants"
	"gitlab.tve-development.com/backend/gtoken/wallet-service/internal/infra/httputil"
)

// CreateKey godoc
// @Summary     Generate new key of role
// @Description Single role has one active key, deposit role key requires external_ref.
// @Tags        admin
// @Accept      json
// @Produce     json
// @Param       id      path string                                true "Role id"
// @Param       request body key_storage.CreateRoleKeyRequest false "Key params"
// @Success     200 {object} httputil.Response{data=key_storage.KeyResponse}
// @Failure     400 {object} httputil.ErrorResponse
// @Failure     404 {object} httputil.ErrorResponse
// @Failure     409 {object} httputil.ErrorResponse
// @Router      /api/v1/admin/roles/{id}/keys [post]
func (h *AdminHandler) CreateKey(c *gin.Context) {
	roleID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		httputil.Fail(c, ErrRoleNotFound)
		return
	}

	var req CreateRoleKeyRequest
	if c.Request.ContentLength != 0 {
		if err := c.ShouldBindJSON(&req); err != nil {
			httputil.BadRequestValidation(c, err)
			return
		}
	}

	key, err := h.service.CreateKey(c.Request.Context(), CreateKeyParams{
		RoleID:      roleID,
		Alias:       req.Alias,
		ExternalRef: req.ExternalRef,
	})
	if err != nil {
		h.log.Error().Err(err).Str("role_id", roleID.String()).Msg("admin create key")
		httputil.Fail(c, err)
		return
	}

	httputil.OK(c, NewKeyResponse(key))
}

// ListRoleKeys godoc
// @Summary     List keys of role
// @Tags        admin
// @Produce     json
// @Param       id path string true "Role id"
// @Success     200 {object} httputil.Response{data=[]key_storage.KeyResponse}
// @Failure     404 {object} httputil.ErrorResponse
// @Router      /api/v1/admin/roles/{id}/keys [get]
func (h *AdminHandler) ListRoleKeys(c *gin.Context) {
	roleID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		httputil.Fail(c, ErrRoleNotFound)
		return
	}

	keys, err := h.service.ListRoleKeys(c.Request.Context(), roleID)
	if err != nil {
		h.log.Error().Err(err).Str("role_id", roleID.String()).Msg("admin list role keys")
		httputil.Fail(c, err)
		return
	}

	httputil.OK(c, NewKeysResponse(keys))
}

// ListKeys godoc
// @Summary     List keys of all services
// @Description Newest first, every filter applies only if parameter is present, so ?label= selects keys with empty label.
// @Tags        admin
// @Produce     json
// @Param       service      query string false "Service name"
// @Param       purpose      query string false "Role purpose"
// @Param       label        query string false "Purpose refinement"
// @Param       network      query string false "Network: ethereum, tron, arbitrum"
// @Param       status       query string false "Status: active, rotating, pending, disabled, revoked"
// @Param       external_ref query string false "Order id of deposit key"
// @Param       limit        query int    false "Max keys, default 100, max 1000"
// @Success     200 {object} httputil.Response{data=[]key_storage.KeyResponse}
// @Failure     400 {object} httputil.ErrorResponse
// @Router      /api/v1/admin/keys [get]
func (h *AdminHandler) ListKeys(c *gin.Context) {
	var query ListKeysQuery
	if err := c.ShouldBindQuery(&query); err != nil {
		httputil.BadRequestValidation(c, err)
		return
	}

	filter := ListKeysFilter{
		Service:     query.Service,
		Purpose:     query.Purpose,
		Label:       query.Label,
		ExternalRef: query.ExternalRef,
		Limit:       query.Limit,
	}
	if query.Network != "" {
		network, _ := constants.ParseNetwork(query.Network)
		filter.Network = &network
	}
	if query.Status != "" {
		status, _ := ParseKeyStatus(query.Status)
		filter.Status = &status
	}

	keys, err := h.service.ListKeys(c.Request.Context(), filter)
	if err != nil {
		h.log.Error().Err(err).Msg("admin list keys")
		httputil.Fail(c, err)
		return
	}

	httputil.OK(c, NewKeysResponse(keys))
}

// RotateKey godoc
// @Summary     Start rotation: create pending replacement of active key
// @Description Old key keeps working and may send its funds to replacement. Switch keys with activate.
// @Tags        admin
// @Produce     json
// @Param       id path string true "Active key id"
// @Success     200 {object} httputil.Response{data=key_storage.KeyResponse} "Replacement key"
// @Failure     400 {object} httputil.ErrorResponse
// @Failure     404 {object} httputil.ErrorResponse
// @Failure     409 {object} httputil.ErrorResponse
// @Router      /api/v1/admin/keys/{id}/rotate [post]
func (h *AdminHandler) RotateKey(c *gin.Context) {
	h.keyAction(c, "rotate", h.service.RotateKey)
}

// ActivateKey godoc
// @Summary     Activate pending key, key replaced by it becomes rotating
// @Tags        admin
// @Produce     json
// @Param       id path string true "Pending key id"
// @Success     200 {object} httputil.Response{data=key_storage.KeyResponse}
// @Failure     404 {object} httputil.ErrorResponse
// @Failure     409 {object} httputil.ErrorResponse
// @Router      /api/v1/admin/keys/{id}/activate [post]
func (h *AdminHandler) ActivateKey(c *gin.Context) {
	h.keyAction(c, "activate", h.service.ActivateKey)
}

// RevokeKey godoc
// @Summary     Revoke key forever, revoking pending key cancels rotation
// @Tags        admin
// @Produce     json
// @Param       id path string true "Key id"
// @Success     200 {object} httputil.Response{data=key_storage.KeyResponse}
// @Failure     404 {object} httputil.ErrorResponse
// @Failure     409 {object} httputil.ErrorResponse
// @Router      /api/v1/admin/keys/{id}/revoke [post]
func (h *AdminHandler) RevokeKey(c *gin.Context) {
	h.keyAction(c, "revoke", h.service.RevokeKey)
}

// DisableKey godoc
// @Summary     Disable active key until enable
// @Tags        admin
// @Produce     json
// @Param       id path string true "Active key id"
// @Success     200 {object} httputil.Response{data=key_storage.KeyResponse}
// @Failure     404 {object} httputil.ErrorResponse
// @Failure     409 {object} httputil.ErrorResponse
// @Router      /api/v1/admin/keys/{id}/disable [post]
func (h *AdminHandler) DisableKey(c *gin.Context) {
	h.keyAction(c, "disable", h.service.DisableKey)
}

// EnableKey godoc
// @Summary     Enable disabled key
// @Tags        admin
// @Produce     json
// @Param       id path string true "Disabled key id"
// @Success     200 {object} httputil.Response{data=key_storage.KeyResponse}
// @Failure     404 {object} httputil.ErrorResponse
// @Failure     409 {object} httputil.ErrorResponse
// @Router      /api/v1/admin/keys/{id}/enable [post]
func (h *AdminHandler) EnableKey(c *gin.Context) {
	h.keyAction(c, "enable", h.service.EnableKey)
}

func (h *AdminHandler) keyAction(c *gin.Context, action string, do func(ctx context.Context, keyID uuid.UUID) (*WalletKey, error)) {
	keyID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		httputil.Fail(c, ErrKeyNotFound)
		return
	}

	key, err := do(c.Request.Context(), keyID)
	if err != nil {
		h.log.Error().
			Err(err).
			Str("key_id", keyID.String()).
			Msg("admin " + action + " key")
		httputil.Fail(c, err)
		return
	}

	httputil.OK(c, NewKeyResponse(key))
}

// ImportKey godoc
// @Summary     Import existing private key into role
// @Description For keys moved from service env. Private key travels in request body: use only over TLS without body logging.
// @Tags        admin
// @Accept      json
// @Produce     json
// @Param       id      path string                           true "Role id"
// @Param       request body key_storage.ImportKeyRequest true "Private key"
// @Success     200 {object} httputil.Response{data=key_storage.KeyResponse}
// @Failure     400 {object} httputil.ErrorResponse
// @Failure     404 {object} httputil.ErrorResponse
// @Failure     409 {object} httputil.ErrorResponse
// @Router      /api/v1/admin/roles/{id}/keys/import [post]
func (h *AdminHandler) ImportKey(c *gin.Context) {
	roleID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		httputil.Fail(c, ErrRoleNotFound)
		return
	}

	var req ImportKeyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		httputil.BadRequestValidation(c, err)
		return
	}

	key, err := h.service.ImportKey(c.Request.Context(), ImportKeyParams{
		RoleID:      roleID,
		PrivateKey:  []byte(req.PrivateKey),
		Alias:       req.Alias,
		ExternalRef: req.ExternalRef,
	})
	if err != nil {
		h.log.Error().
			Err(err).
			Str("role_id", roleID.String()).
			Msg("admin import key")
		httputil.Fail(c, err)
		return
	}

	httputil.OK(c, NewKeyResponse(key))
}
