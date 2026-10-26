package signing

import (
	"context"
	"encoding/hex"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"gitlab.tve-development.com/backend/gtoken/wallet-service/internal/app/middleware"
	"gitlab.tve-development.com/backend/gtoken/wallet-service/internal/constants"
	keystorage "gitlab.tve-development.com/backend/gtoken/wallet-service/internal/features/key_storage"
	"gitlab.tve-development.com/backend/gtoken/wallet-service/internal/infra/httputil"
	"gitlab.tve-development.com/backend/gtoken/wallet-service/internal/infra/logger"
)

type SigningService interface {
	SignTransaction(ctx context.Context, params SignTransactionParams) (*SignedTransaction, error)
}

type Handler struct {
	service SigningService
	log     *logger.Logger
}

func NewHandler(service SigningService, log *logger.Logger) *Handler {
	return &Handler{service: service, log: log}
}

// SignTransaction godoc
// @Summary     Sign transaction
// @Description Signs unsigned tx with own key. Tx is built and sent by the caller.
// @Tags        signing
// @Accept      json
// @Produce     json
// @Param       id      path string                            true "Key id"
// @Param       request body signing.SignTransactionRequest true "Unsigned tx"
// @Success     200 {object} httputil.Response{data=signing.SignTransactionResponse}
// @Failure     400 {object} httputil.ErrorResponse
// @Failure     401 {object} httputil.ErrorResponse
// @Failure     403 {object} httputil.ErrorResponse
// @Failure     404 {object} httputil.ErrorResponse
// @Failure     409 {object} httputil.ErrorResponse
// @Security    ServiceKey
// @Router      /api/v1/keys/{id}/sign-transaction [post]
func (h *Handler) SignTransaction(c *gin.Context) {
	service, err := middleware.GetService(c)
	if err != nil {
		httputil.Fail(c, err)
		return
	}

	keyID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		httputil.Fail(c, keystorage.ErrKeyNotFound)
		return
	}

	var req SignTransactionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		httputil.BadRequestValidation(c, err)
		return
	}

	network, _ := constants.ParseNetwork(req.Network)

	rawTx, err := hex.DecodeString(strings.TrimPrefix(req.RawTx, "0x"))
	if err != nil {
		httputil.Fail(c, ErrInvalidTransaction)
		return
	}

	signed, err := h.service.SignTransaction(c.Request.Context(), SignTransactionParams{
		Service: service,
		KeyID:   keyID,
		Network: network,
		RawTx:   rawTx,
		LeaseID: req.LeaseID,
	})
	if err != nil {
		h.log.Error().Err(err).Str("key_id", keyID.String()).Msg("sign transaction")
		httputil.Fail(c, err)
		return
	}

	httputil.OK(c, SignTransactionResponse{
		SignedTx: "0x" + hex.EncodeToString(signed.SignedTx),
		TxHash:   signed.TxHash,
	})
}
