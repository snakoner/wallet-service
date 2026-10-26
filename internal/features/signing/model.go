package signing

import (
	"github.com/google/uuid"

	"gitlab.tve-development.com/backend/gtoken/wallet-service/internal/constants"
)

type SignTransactionParams struct {
	Service string
	KeyID   uuid.UUID
	Network constants.Network
	RawTx   []byte
	LeaseID *uuid.UUID
}

type SignedTransaction struct {
	SignedTx []byte
	TxHash   string
}
