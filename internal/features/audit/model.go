package audit

import (
	"time"

	"github.com/google/uuid"

	"gitlab.tve-development.com/backend/gtoken/wallet-service/internal/constants"
)

type SigningRecord struct {
	ID              uuid.UUID
	CreatedAt       time.Time
	ServiceClientID uuid.UUID
	KeyID           uuid.UUID
	Network         constants.Network
	RawTx           []byte
	TxHash          *string // signed
	ErrorCode       *string // denied or failed
}
