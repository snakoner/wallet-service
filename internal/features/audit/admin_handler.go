package audit

import (
	"context"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"gitlab.tve-development.com/backend/gtoken/wallet-service/internal/infra/httputil"
	"gitlab.tve-development.com/backend/gtoken/wallet-service/internal/infra/logger"
)

type AdminService interface {
	ListSigning(ctx context.Context, filter ListSigningFilter) ([]SigningRecord, int64, error)
}

type AdminHandler struct {
	service AdminService
	log     *logger.Logger
}

func NewAdminHandler(service AdminService, log *logger.Logger) *AdminHandler {
	return &AdminHandler{service: service, log: log}
}

// ListSigning godoc
// @Summary     Signing audit: every sign-transaction request, signed or denied
// @Description Newest first, empty parameters are not applied.
// @Tags        admin
// @Produce     json
// @Param       key_id     query string false "Key id"
// @Param       service    query string false "Service of caller"
// @Param       tx_hash    query string false "Hash of signed tx"
// @Param       error_code query string false "Error code of response, e.g. POLICY_VIOLATION"
// @Param       failed     query bool   false "true - only denied or failed, false - only signed"
// @Param       from       query string false "Created at or after, RFC3339"
// @Param       to         query string false "Created before, RFC3339"
// @Param       offset     query int    false "Records to skip, default 0"
// @Param       limit      query int    false "Max records, default 100, max 1000"
// @Success     200 {object} httputil.Response{data=audit.SigningPageResponse}
// @Failure     400 {object} httputil.ErrorResponse
// @Router      /api/v1/admin/audit [get]
func (h *AdminHandler) ListSigning(c *gin.Context) {
	var query ListSigningQuery
	if err := c.ShouldBindQuery(&query); err != nil {
		httputil.BadRequestValidation(c, err)
		return
	}

	offset, limit := httputil.ParsePagination(c)
	filter := ListSigningFilter{
		Service:   query.Service,
		TxHash:    query.TxHash,
		ErrorCode: query.ErrorCode,
		Failed:    query.Failed,
		From:      query.From,
		To:        query.To,
		Offset:    offset,
		Limit:     limit,
	}

	if query.KeyID != "" {
		filter.KeyID, _ = uuid.Parse(query.KeyID)
	}

	records, total, err := h.service.ListSigning(c.Request.Context(), filter)
	if err != nil {
		h.log.Error().Err(err).Msg("admin list signing audit")
		httputil.Fail(c, err)
		return
	}

	httputil.OK(c, NewSigningPageResponse(records, total))
}
