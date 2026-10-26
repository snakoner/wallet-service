package key_storage

const (
	roleUniqueConstraint           = "wallet_roles_unique"
	keyAddressUniqueConstraint     = "wallet_keys_network_address_unique"
	keyExternalRefUniqueConstraint = "wallet_keys_role_external_ref_unique"
)

// nowUTC is current time in the same zone as stored timestamps.
const nowUTC = `(now() AT TIME ZONE 'UTC')`

type Repository struct{}

func NewRepository() *Repository {
	return &Repository{}
}
