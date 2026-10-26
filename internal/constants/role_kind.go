package constants

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
