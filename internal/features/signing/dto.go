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
}

type SignedTransaction struct {
	SignedTx []byte
	TxHash   string
}

type SignTransactionRequest struct {
	Network string `json:"network" binding:"required,network"`
	RawTx   string `json:"raw_tx" binding:"required"` // hex of unsigned tx: ethereum - types.Transaction.MarshalBinary, tron - raw_data_hex
}

type SignTransactionResponse struct {
	SignedTx string `json:"signed_tx"` // hex: ethereum - eth_sendRawTransaction, tron - /wallet/broadcasthex
	TxHash   string `json:"tx_hash"`
}
