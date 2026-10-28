package audit

import (
	"encoding/hex"
	"time"

	"github.com/google/uuid"
)

type ListSigningQuery struct {
	KeyID     string    `form:"key_id" binding:"omitempty,uuid"`
	Service   string    `form:"service"`
	TxHash    string    `form:"tx_hash"`
	ErrorCode string    `form:"error_code"`
	Failed    *bool     `form:"failed"`
	From      time.Time `form:"from" time_format:"2006-01-02T15:04:05Z07:00"`
	To        time.Time `form:"to" time_format:"2006-01-02T15:04:05Z07:00"`
}

type SigningRecordResponse struct {
	ID              uuid.UUID `json:"id"`
	CreatedAt       time.Time `json:"created_at"`
	ServiceClientID uuid.UUID `json:"service_client_id"`
	Service         string    `json:"service"`
	KeyID           uuid.UUID `json:"key_id"`
	Network         string    `json:"network"`
	RawTx           string    `json:"raw_tx"` // unsigned tx as received, hex with 0x
	TxHash          *string   `json:"tx_hash,omitempty"`
	ErrorCode       *string   `json:"error_code,omitempty"`
}

type SigningPageResponse struct {
	Records []SigningRecordResponse `json:"records"`
	Total   int64                   `json:"total"`
}

func NewSigningPageResponse(records []SigningRecord, total int64) SigningPageResponse {
	return SigningPageResponse{Records: NewSigningRecordsResponse(records), Total: total}
}

func NewSigningRecordsResponse(records []SigningRecord) []SigningRecordResponse {
	resp := make([]SigningRecordResponse, len(records))
	for i, record := range records {
		resp[i] = SigningRecordResponse{
			ID:              record.ID,
			CreatedAt:       record.CreatedAt,
			ServiceClientID: record.ServiceClientID,
			Service:         record.Service,
			KeyID:           record.KeyID,
			Network:         record.Network.String(),
			RawTx:           "0x" + hex.EncodeToString(record.RawTx),
			TxHash:          record.TxHash,
			ErrorCode:       record.ErrorCode,
		}
	}

	return resp
}
