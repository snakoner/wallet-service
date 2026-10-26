package constants

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
