package key_storage

import (
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"gitlab.tve-development.com/backend/gtoken/wallet-service/internal/constants"
	"gitlab.tve-development.com/backend/gtoken/wallet-service/internal/infra/httputil"
)

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
	kind, _ := constants.ParseRoleKind(req.Kind)

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

// GetRole godoc
// @Summary     Role card: role with policy and number of keys by status
// @Tags        admin
// @Produce     json
// @Param       id path string true "Role id"
// @Success     200 {object} httputil.Response{data=key_storage.RoleDetailsResponse}
// @Failure     404 {object} httputil.ErrorResponse
// @Router      /api/v1/admin/roles/{id} [get]
func (h *AdminHandler) GetRole(c *gin.Context) {
	roleID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		httputil.Fail(c, ErrRoleNotFound)
		return
	}

	role, counts, err := h.service.GetRole(c.Request.Context(), roleID)
	if err != nil {
		h.log.Error().Err(err).Str("role_id", roleID.String()).Msg("admin get role")
		httputil.Fail(c, err)
		return
	}

	httputil.OK(c, NewRoleDetailsResponse(role, counts))
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
