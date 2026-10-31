package admin_audit

import (
	"context"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"gitlab.tve-development.com/backend/gtoken/wallet-service/internal/infra/httputil"
	"gitlab.tve-development.com/backend/gtoken/wallet-service/internal/infra/logger"
)

type AdminService interface {
	List(ctx context.Context, filter ListFilter) ([]Record, int64, error)
}

type AdminHandler struct {
	service AdminService
	log     *logger.Logger
}

func NewAdminHandler(service AdminService, log *logger.Logger) *AdminHandler {
	return &AdminHandler{service: service, log: log}
}

// List godoc
// @Summary     Admin audit: who changed roles, policies, keys and service keys
// @Description Newest first, empty parameters are not applied. key_id also matches rotation records where key is replacement or replaced key.
// @Tags        admin
// @Produce     json
// @Param       actor             query string false "Admin"
// @Param       action            query string false "Action, e.g. key.revoke, role.policy_update"
// @Param       service           query string false "Service of changed object"
// @Param       role_id           query string false "Role id"
// @Param       key_id            query string false "Key id"
// @Param       service_client_id query string false "Service client id"
// @Param       from              query string false "Created at or after, RFC3339"
// @Param       to                query string false "Created before, RFC3339"
// @Param       offset            query int    false "Records to skip, default 0"
// @Param       limit             query int    false "Max records, default 100, max 1000"
// @Success     200 {object} httputil.Response{data=admin_audit.PageResponse}
// @Failure     400 {object} httputil.ErrorResponse
// @Router      /api/v1/admin/audit/actions [get]
func (h *AdminHandler) List(c *gin.Context) {
	var query ListQuery
	if err := c.ShouldBindQuery(&query); err != nil {
		httputil.BadRequestValidation(c, err)
		return
	}

	offset, limit := httputil.ParsePagination(c)
	filter := ListFilter{
		Actor:   query.Actor,
		Action:  query.Action,
		Service: query.Service,
		From:    query.From,
		To:      query.To,
		Offset:  offset,
		Limit:   limit,
	}

	// validated by binding
	filter.RoleID, _ = uuid.Parse(query.RoleID)
	filter.KeyID, _ = uuid.Parse(query.KeyID)
	filter.ServiceClientID, _ = uuid.Parse(query.ServiceClientID)

	records, total, err := h.service.List(c.Request.Context(), filter)
	if err != nil {
		h.log.Error().Err(err).Msg("admin list admin audit")
		httputil.Fail(c, err)
		return
	}

	httputil.OK(c, NewPageResponse(records, total))
}
