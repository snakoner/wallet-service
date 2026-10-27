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

var RoleKinds = make(map[string]RoleKind, len(RoleKindStrings))

func init() {
	for kind, name := range RoleKindStrings {
		RoleKinds[name] = kind
	}
}

func (k RoleKind) String() string {
	return RoleKindStrings[k]
}

func ParseRoleKind(s string) (RoleKind, bool) {
	if kind, ok := RoleKinds[s]; ok {
		return kind, true
	}

	return RoleKindSingle, false
}
