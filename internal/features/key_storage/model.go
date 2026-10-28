package key_storage

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"

	"gitlab.tve-development.com/backend/gtoken/wallet-service/internal/constants"
)

type Role struct {
	ID        uuid.UUID
	Service   string
	Purpose   string
	Label     string
	Network   constants.Network
	Kind      constants.RoleKind
	Policy    json.RawMessage
	CreatedAt time.Time
}

type WalletKey struct {
	ID                 uuid.UUID
	RoleID             uuid.UUID
	Role               Role
	Network            constants.Network
	Alias              *string
	Address            string
	ExternalRef        *string
	Status             constants.KeyStatus
	KeyCipher          []byte
	DEKCipher          []byte
	KEKVersion         int32
	ReplacedBy         *uuid.UUID // new key after rotation
	ReplacementAddress *string
	LeaseID            *uuid.UUID
	LeaseOwner         *string
	LeasedUntil        *time.Time
	LeaseActive        bool // lease exists and not expired, loaded for signing and in lists
	LeaseEnding        bool // lease expires within 10 seconds: too late to sign with it, loaded only for signing
	CreatedAt          time.Time
	UpdatedAt          time.Time
	RotatedAt          *time.Time
	RevokedAt          *time.Time
}

type Lease struct {
	ID        uuid.UUID
	ExpiresAt time.Time
	Key       WalletKey
}

type CreateRoleParams struct {
	Service string
	Purpose string
	Label   string
	Network constants.Network
	Kind    constants.RoleKind
	Policy  json.RawMessage
}

type CreateKeyParams struct {
	RoleID      uuid.UUID
	Alias       *string
	ExternalRef *string
}

type ImportKeyParams struct {
	RoleID      uuid.UUID
	PrivateKey  []byte // hex, cleared after import
	Alias       *string
	ExternalRef *string
}

type ListKeysFilter struct {
	ID          *uuid.UUID
	RoleID      *uuid.UUID
	Service     *string
	Purpose     *string
	Label       *string
	Network     *constants.Network
	Status      *constants.KeyStatus
	ExternalRef *string
	Address     *string // case-insensitive
	Leased      *bool   // true - under live lease, false - free
	Offset      int
	Limit       int
}

type GetRoleKeyParams struct {
	Service string
	Purpose string
	Label   string
	Network constants.Network
}

type GetDepositKeyParams struct {
	Service     string
	Purpose     string
	Label       string
	Network     constants.Network
	ExternalRef string
	Alias       *string
}

type AcquireLeaseParams struct {
	Service string
	Purpose string
	Label   string
	Network constants.Network
	Owner   string
	TTL     time.Duration
	Exclude []uuid.UUID
}
