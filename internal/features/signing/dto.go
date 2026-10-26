package signing

import "github.com/google/uuid"

type SignTransactionRequest struct {
	Network string     `json:"network" binding:"required,network"`
	RawTx   string     `json:"raw_tx" binding:"required"` // hex of unsigned tx: ethereum - types.Transaction.MarshalBinary, tron - raw_data_hex
	LeaseID *uuid.UUID `json:"lease_id"`                  // required for pool keys
}

type SignTransactionResponse struct {
	SignedTx string `json:"signed_tx"` // hex: ethereum - eth_sendRawTransaction, tron - /wallet/broadcasthex
	TxHash   string `json:"tx_hash"`
}
