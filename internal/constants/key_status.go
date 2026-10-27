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

var KeyStatuses = make(map[string]KeyStatus, len(KeyStatusStrings))

func init() {
	for status, name := range KeyStatusStrings {
		KeyStatuses[name] = status
	}
}

func (s KeyStatus) String() string {
	return KeyStatusStrings[s]
}

func ParseKeyStatus(s string) (KeyStatus, bool) {
	if status, ok := KeyStatuses[s]; ok {
		return status, true
	}

	return KeyStatusActive, false
}
