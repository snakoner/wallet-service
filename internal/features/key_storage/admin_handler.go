package key_storage

import (
	"context"
	"encoding/json"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"gitlab.tve-development.com/backend/gtoken/wallet-service/internal/constants"
	"gitlab.tve-development.com/backend/gtoken/wallet-service/internal/infra/httputil"
	"gitlab.tve-development.com/backend/gtoken/wallet-service/internal/infra/logger"
)

type AdminService interface {
	CreateRole(ctx context.Context, params CreateRoleParams) (*Role, error)
	ListRoles(ctx context.Context, service string) ([]Role, error)
	UpdatePolicy(ctx context.Context, roleID uuid.UUID, raw json.RawMessage) (*Role, error)
	CreateKey(ctx context.Context, params CreateKeyParams) (*WalletKey, error)
	ListRoleKeys(ctx context.Context, roleID uuid.UUID) ([]WalletKey, error)
	ListKeys(ctx context.Context, filter ListKeysFilter) ([]WalletKey, error)
	RotateKey(ctx context.Context, keyID uuid.UUID) (*WalletKey, error)
	ActivateKey(ctx context.Context, keyID uuid.UUID) (*WalletKey, error)
	RevokeKey(ctx context.Context, keyID uuid.UUID) (*WalletKey, error)
	DisableKey(ctx context.Context, keyID uuid.UUID) (*WalletKey, error)
	EnableKey(ctx context.Context, keyID uuid.UUID) (*WalletKey, error)
}

type AdminHandler struct {
	service AdminService
	log     *logger.Logger
}

func NewAdminHandler(service AdminService, log *logger.Logger) *AdminHandler {
	return &AdminHandler{service: service, log: log}
}

// CreateRole godoc
// @Summary     Create wallet role of service
// @Tags        admin
// @Accept      json
// @Produce     json
// @Param       request body key_storage.CreateRoleRequest true "Role params"
// @Success     200 {object} httputil.Response{data=key_storage.RoleResponse}
// @Failure     400 {object} httputil.ErrorResponse
// @Failure     409 {object} httputil.ErrorResponse
// @Router      /api/v1/admin/roles [post]
func (h *AdminHandler) CreateRole(c *gin.Context) {
	var req CreateRoleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		httputil.BadRequestValidation(c, err)
		return
	}

	network, _ := constants.ParseNetwork(req.Network)
	kind, _ := ParseRoleKind(req.Kind)

	role, err := h.service.CreateRole(c.Request.Context(), CreateRoleParams{
		Service: req.Service,
		Purpose: req.Purpose,
		Label:   req.Label,
		Network: network,
		Kind:    kind,
		Policy:  req.Policy,
	})
	if err != nil {
		h.log.Error().Err(err).Msg("admin create role")
		httputil.Fail(c, err)
		return
	}

	httputil.OK(c, NewRoleResponse(role))
}

// ListRoles godoc
// @Summary     List wallet roles
// @Tags        admin
// @Produce     json
// @Param       service query string false "Service name, all services when empty"
// @Success     200 {object} httputil.Response{data=[]key_storage.RoleResponse}
// @Router      /api/v1/admin/roles [get]
func (h *AdminHandler) ListRoles(c *gin.Context) {
	var query ListRolesQuery
	if err := c.ShouldBindQuery(&query); err != nil {
		httputil.BadRequestValidation(c, err)
		return
	}

	roles, err := h.service.ListRoles(c.Request.Context(), query.Service)
	if err != nil {
		h.log.Error().Err(err).Msg("admin list roles")
		httputil.Fail(c, err)
		return
	}

	httputil.OK(c, NewRolesResponse(roles))
}

// UpdatePolicy godoc
// @Summary     Replace signing policy of role
// @Description Applies to all keys of role. Format is described in docs/POLICY.md. Empty rules deny signing.
// @Tags        admin
// @Accept      json
// @Produce     json
// @Param       id      path string                             true "Role id"
// @Param       request body key_storage.UpdatePolicyRequest true "Policy"
// @Success     200 {object} httputil.Response{data=key_storage.RoleResponse}
// @Failure     400 {object} httputil.ErrorResponse
// @Failure     404 {object} httputil.ErrorResponse
// @Router      /api/v1/admin/roles/{id}/policy [put]
func (h *AdminHandler) UpdatePolicy(c *gin.Context) {
	roleID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		httputil.Fail(c, ErrRoleNotFound)
		return
	}

	var req UpdatePolicyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		httputil.BadRequestValidation(c, err)
		return
	}

	role, err := h.service.UpdatePolicy(c.Request.Context(), roleID, req.Policy)
	if err != nil {
		h.log.Error().Err(err).Str("role_id", roleID.String()).Msg("admin update policy")
		httputil.Fail(c, err)
		return
	}

	httputil.OK(c, NewRoleResponse(role))
}

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
		h.log.Error().Err(err).Str("key_id", keyID.String()).Msg("admin " + action + " key")
		httputil.Fail(c, err)
		return
	}

	httputil.OK(c, NewKeyResponse(key))
}
