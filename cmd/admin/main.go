package main

import (
	"bufio"
	"context"
	"encoding/hex"
	"flag"
	"fmt"
	"os"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	aes256 "github.com/w3-developers/go-aes256"

	"gitlab.tve-development.com/backend/gtoken/wallet-service/internal/config"
	"gitlab.tve-development.com/backend/gtoken/wallet-service/internal/constants"
	keystorage "gitlab.tve-development.com/backend/gtoken/wallet-service/internal/features/key_storage"
	serviceclient "gitlab.tve-development.com/backend/gtoken/wallet-service/internal/features/service_client"
	"gitlab.tve-development.com/backend/gtoken/wallet-service/internal/infra/db"
	"gitlab.tve-development.com/backend/gtoken/wallet-service/internal/infra/logger"
)

const usage = `usage:
  admin create-service-client -service <name>   register service and print its X-Service-Key
  admin create-role -service <name> -purpose <purpose> [-label <label>] -network <ethereum|tron|arbitrum> -kind <single|pool|deposit> [-policy <file.json>]
  admin set-policy -role <role_id> -policy <file.json>   replace signing policy of role
  admin create-key -role <role_id> [-alias <name>] [-external-ref <ref>]   generate new key of role
  admin import-key -role <role_id> [-alias <name>] [-external-ref <ref>]   import private key hex from stdin
`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}

	switch os.Args[1] {
	case "create-service-client":
		createServiceClient(os.Args[2:])
	case "create-role":
		createRole(os.Args[2:])
	case "set-policy":
		setPolicy(os.Args[2:])
	case "create-key":
		createKey(os.Args[2:], false)
	case "import-key":
		createKey(os.Args[2:], true)
	default:
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
}

type admin struct {
	cfg  *config.Config
	log  *logger.Logger
	pool *pgxpool.Pool
	txm  db.TxManager
}

func newAdmin(ctx context.Context) *admin {
	cfg := config.MustLoad()
	log := logger.New(logger.Config{
		Level:       cfg.Log.Level,
		ServiceName: "wallet-service-admin",
		Environment: cfg.App.Environment,
		Pretty:      cfg.Log.Pretty,
	})

	pool, err := pgxpool.New(ctx, cfg.DB.DSN())
	if err != nil {
		log.Fatal().Err(err).Msg("failed to connect to postgres")
	}

	return &admin{cfg: cfg, log: log, pool: pool, txm: db.NewPgxTxManager(pool)}
}

func (a *admin) keyStorage() *keystorage.Service {
	kek, err := hex.DecodeString(a.cfg.KeyStorage.KEK)
	if err != nil || len(kek) != 32 {
		a.log.Fatal().Msg("KEY_STORAGE_KEK must be 32 bytes in hex")
	}

	return keystorage.NewService(a.log, keystorage.NewRepository(), a.txm, aes256.NewAES256GCM(kek), a.cfg.KeyStorage.KEKVersion)
}

func createServiceClient(args []string) {
	fs := flag.NewFlagSet("create-service-client", flag.ExitOnError)
	service := fs.String("service", "", "service name, e.g. exchange")
	_ = fs.Parse(args)

	ctx := context.Background()
	a := newAdmin(ctx)
	defer a.pool.Close()

	srv := serviceclient.NewService(a.log, serviceclient.NewRepository(), a.txm)

	key, client, err := srv.CreateClient(ctx, *service)
	if err != nil {
		a.log.Fatal().Err(err).Msg("failed to create service client")
	}

	fmt.Printf("service:            %s\n", client.Service)
	fmt.Printf("service_client_id:  %s\n", client.ID)
	fmt.Printf("X-Service-Key:      %s\n", key)
	fmt.Println("сохраните ключ сейчас: в базе хранится только его хэш")
}

func createRole(args []string) {
	fs := flag.NewFlagSet("create-role", flag.ExitOnError)
	service := fs.String("service", "", "service name, e.g. exchange")
	purpose := fs.String("purpose", "", "role purpose, e.g. gas_station")
	label := fs.String("label", "", "purpose refinement, e.g. atoken")
	networkName := fs.String("network", "", "ethereum, tron, arbitrum")
	kindName := fs.String("kind", "", "single, pool, deposit")
	policyFile := fs.String("policy", "", "path to signing policy json")
	_ = fs.Parse(args)

	ctx := context.Background()
	a := newAdmin(ctx)
	defer a.pool.Close()

	network, ok := constants.ParseNetwork(*networkName)
	if !ok {
		a.log.Fatal().Str("network", *networkName).Msg("unsupported network")
	}

	kind, ok := keystorage.ParseRoleKind(*kindName)
	if !ok {
		a.log.Fatal().Str("kind", *kindName).Msg("unsupported role kind")
	}

	var policy []byte
	if *policyFile != "" {
		var err error
		if policy, err = os.ReadFile(*policyFile); err != nil {
			a.log.Fatal().Err(err).Msg("failed to read policy")
		}
	}

	role, err := a.keyStorage().CreateRole(ctx, keystorage.CreateRoleParams{
		Service: *service,
		Purpose: *purpose,
		Label:   *label,
		Network: network,
		Kind:    kind,
		Policy:  policy,
	})
	if err != nil {
		a.log.Fatal().Err(err).Msg("failed to create role")
	}

	fmt.Printf("role_id:  %s\n", role.ID)
	fmt.Printf("role:     %s/%s/%s/%s (%s)\n", role.Service, role.Purpose, role.Label, role.Network, role.Kind)
}

func setPolicy(args []string) {
	fs := flag.NewFlagSet("set-policy", flag.ExitOnError)
	roleID := fs.String("role", "", "role id")
	policyFile := fs.String("policy", "", "path to signing policy json")
	_ = fs.Parse(args)

	ctx := context.Background()
	a := newAdmin(ctx)
	defer a.pool.Close()

	id, err := uuid.Parse(*roleID)
	if err != nil {
		a.log.Fatal().Str("role", *roleID).Msg("invalid role id")
	}

	policy, err := os.ReadFile(*policyFile)
	if err != nil {
		a.log.Fatal().Err(err).Msg("failed to read policy")
	}

	role, err := a.keyStorage().UpdatePolicy(ctx, id, policy)
	if err != nil {
		a.log.Fatal().Err(err).Msg("failed to update policy")
	}

	fmt.Printf("role_id:  %s\n", role.ID)
	fmt.Printf("policy:   %s\n", role.Policy)
}

func createKey(args []string, imported bool) {
	fs := flag.NewFlagSet("create-key", flag.ExitOnError)
	roleID := fs.String("role", "", "role id")
	alias := fs.String("alias", "", "optional human-readable name")
	externalRef := fs.String("external-ref", "", "order id, only for deposit role")
	_ = fs.Parse(args)

	ctx := context.Background()
	a := newAdmin(ctx)
	defer a.pool.Close()

	id, err := uuid.Parse(*roleID)
	if err != nil {
		a.log.Fatal().Str("role", *roleID).Msg("invalid role id")
	}

	var key *keystorage.WalletKey
	if imported {
		fmt.Fprintln(os.Stderr, "введите приватный ключ (hex) и нажмите Enter:")
		scanner := bufio.NewScanner(os.Stdin)
		if !scanner.Scan() {
			a.log.Fatal().Msg("failed to read private key from stdin")
		}

		key, err = a.keyStorage().ImportKey(ctx, keystorage.ImportKeyParams{
			RoleID:      id,
			PrivateKey:  scanner.Bytes(),
			Alias:       optional(*alias),
			ExternalRef: optional(*externalRef),
		})
	} else {
		key, err = a.keyStorage().CreateKey(ctx, keystorage.CreateKeyParams{
			RoleID:      id,
			Alias:       optional(*alias),
			ExternalRef: optional(*externalRef),
		})
	}
	if err != nil {
		a.log.Fatal().Err(err).Msg("failed to create key")
	}

	fmt.Printf("key_id:   %s\n", key.ID)
	fmt.Printf("address:  %s\n", key.Address)
}

func optional(s string) *string {
	if s == "" {
		return nil
	}

	return &s
}
