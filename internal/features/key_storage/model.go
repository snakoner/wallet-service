package key_storage

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"

	"gitlab.tve-development.com/backend/gtoken/wallet-service/internal/constants"
)

type RoleKind int8

const (
	RoleKindSingle  RoleKind = 1 // one active key: gas station, executor
	RoleKindPool    RoleKind = 2 // several interchangeable active keys: executors
	RoleKindDeposit RoleKind = 3 // key per external_ref: deposit wallet of order
)

var RoleKindStrings = map[RoleKind]string{
	RoleKindSingle:  "single",
	RoleKindPool:    "pool",
	RoleKindDeposit: "deposit",
}

func (k RoleKind) String() string {
	return RoleKindStrings[k]
}

func ParseRoleKind(s string) (RoleKind, bool) {
	for kind, name := range RoleKindStrings {
		if name == s {
			return kind, true
		}
	}

	return RoleKindSingle, false
}

type Role struct {
	ID        uuid.UUID
	Service   string
	Purpose   string
	Label     string
	Network   constants.Network
	Kind      RoleKind
	Policy    json.RawMessage
	CreatedAt time.Time
}

type KeyStatus int8

const (
	KeyStatusActive   KeyStatus = 0 // active
	KeyStatusRotating KeyStatus = 1 // rotating, but there is unsend txs
	KeyStatusPending  KeyStatus = 2 // new key after rotating, no gas
	KeyStatusDisabled KeyStatus = 3 // manually disabled, can become active again
	KeyStatusRevoked  KeyStatus = 4 // rotating -> revoked
)

var KeyStatusStrings = map[KeyStatus]string{
	KeyStatusActive:   "active",
	KeyStatusRotating: "rotating",
	KeyStatusPending:  "pending",
	KeyStatusDisabled: "disabled",
	KeyStatusRevoked:  "revoked",
}

func (s KeyStatus) String() string {
	return KeyStatusStrings[s]
}

func ParseKeyStatus(s string) (KeyStatus, bool) {
	for status, name := range KeyStatusStrings {
		if name == s {
			return status, true
		}
	}

	return KeyStatusActive, false
}

type WalletKey struct {
	ID                 uuid.UUID
	RoleID             uuid.UUID
	Role               Role
	Network            constants.Network
	Alias              *string
	Address            string
	ExternalRef        *string
	Status             KeyStatus
	KeyCipher          []byte
	DEKCipher          []byte
	KEKVersion         int32
	ReplacedBy         *uuid.UUID // new key after rotation
	ReplacementAddress *string
	LeaseID            *uuid.UUID
	LeaseOwner         *string
	LeasedUntil        *time.Time
	LeaseActive        bool // lease exists and not expired, loaded only for signing
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
	Kind    RoleKind
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
	Service     *string
	Purpose     *string
	Label       *string
	Network     *constants.Network
	Status      *KeyStatus
	ExternalRef *string
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
