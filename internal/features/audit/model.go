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
	Service         string // of service client, filled on list
	KeyID           uuid.UUID
	Network         constants.Network
	RawTx           []byte
	TxHash          *string // signed
	ErrorCode       *string // denied or failed
}

type ListSigningFilter struct {
	KeyID     uuid.UUID
	Service   string
	TxHash    string
	ErrorCode string
	Failed    *bool // true - only denied or failed, false - only signed
	From      time.Time
	To        time.Time // exclusive
	Offset    int
	Limit     int
}
