package key_storage

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"

	"gitlab.tve-development.com/backend/gtoken/wallet-service/internal/constants"
)

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
	CreatedAt          time.Time
	UpdatedAt          time.Time
	RotatedAt          *time.Time
	RevokedAt          *time.Time
}
