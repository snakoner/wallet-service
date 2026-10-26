package app

import (
	keystorage "gitlab.tve-development.com/backend/gtoken/wallet-service/internal/features/key_storage"
	serviceclient "gitlab.tve-development.com/backend/gtoken/wallet-service/internal/features/service_client"
	"gitlab.tve-development.com/backend/gtoken/wallet-service/internal/features/signing"
)

type Handlers struct {
	KeyStorage   *keystorage.Handler
	Signing      *signing.Handler
	Admin        *keystorage.AdminHandler
	ClientsAdmin *serviceclient.AdminHandler
}
