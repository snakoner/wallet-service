package main

import (
	"bufio"
	"context"
	"encoding/hex"
	"flag"
	"fmt"
	"os"
	"text/tabwriter"
	"time"

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

const usage = `wallet-admin - manage services, roles and keys of wallet service

Usage:
  wallet-admin <command> [flags]

Services:
  create-service-client   register service, print its X-Service-Key once
      -service <name>         service name, e.g. exchange

  rotate-service-client   issue new X-Service-Key, old one works until grace period ends
      -service <name>
      -grace <duration>       how long old key is accepted, default 24h, 0 rejects it at once

  revoke-service-client   reject key at once, e.g. after leak
      -id <service_client_id>

  list-service-clients    list keys of services without hashes
      -service <name>         optional, all services when empty

Roles:
  create-role             create wallet role of service
      -service <name>         service name, e.g. exchange
      -purpose <purpose>      e.g. gas_station, executor, deposit
      -label <label>          optional purpose refinement, e.g. atoken
      -network <network>      ethereum | tron | arbitrum
      -kind <kind>            single | pool | deposit
      -policy <file.json>     optional signing policy, empty denies signing

  list-roles              list roles with their policies
      -service <name>         optional, all services when empty

  set-policy              replace signing policy of role, applies to all its keys
      -role <role_id>
      -policy <file.json>

Keys:
  create-key              generate new key of role, print its address
      -role <role_id>
      -alias <name>           optional human-readable name
      -external-ref <ref>     order id, required only for deposit role

  list-keys               list keys of all services, newest first, filters apply only if flag is set
      -service <name>
      -purpose <purpose>
      -label <label>          '' is a valid label
      -network <network>      ethereum | tron | arbitrum
      -status <status>        active | rotating | pending | disabled | revoked
      -external-ref <ref>
      -limit <n>              default 100, max 1000

  rotate-key              start rotation: create pending replacement of active key, print its address
      -id <key_id>            old key keeps working and may send its funds to replacement

  activate-key            activate pending key, key replaced by it becomes rotating
      -id <key_id>

  revoke-key              forbid signing forever, revoking pending key cancels rotation
      -id <key_id>

  disable-key / enable-key  stop signing with active key until enable
      -id <key_id>

  release-lease           free key held by stuck service instance
      -id <key_id>

  import-key              import existing private key (hex) read from stdin
      -role <role_id>
      -alias <name>
      -external-ref <ref>

Examples:
  wallet-admin create-service-client -service exchange
  wallet-admin rotate-service-client -service exchange -grace 24h
  wallet-admin create-role -service exchange -purpose gas_station -network ethereum -kind single -policy /policies/atoken-gas-station-ethereum.json
  wallet-admin list-roles -service exchange
  wallet-admin set-policy -role <role_id> -policy /policies/atoken-executor-ethereum.json
  wallet-admin create-key -role <role_id>
  wallet-admin list-keys -service exchange -purpose executor -network ethereum -status active
  wallet-admin import-key -role <role_id>    then paste key and press Enter
  wallet-admin rotate-key -id <key_id>       then fund replacement and activate-key -id <new_key_id>
`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}

	switch os.Args[1] {
	case "help", "-h", "--help":
		fmt.Print(usage)
	case "create-service-client":
		createServiceClient(os.Args[2:])
	case "rotate-service-client":
		rotateServiceClient(os.Args[2:])
	case "revoke-service-client":
		revokeServiceClient(os.Args[2:])
	case "list-service-clients":
		listServiceClients(os.Args[2:])
	case "create-role":
		createRole(os.Args[2:])
	case "list-roles":
		listRoles(os.Args[2:])
	case "set-policy":
		setPolicy(os.Args[2:])
	case "list-keys":
		listKeys(os.Args[2:])
	case "release-lease":
		releaseLease(os.Args[2:])
	case "rotate-key", "activate-key", "revoke-key", "disable-key", "enable-key":
		keyAction(os.Args[1], os.Args[2:])
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
	fmt.Println("save the key now: only its hash is stored")
}

func rotateServiceClient(args []string) {
	fs := flag.NewFlagSet("rotate-service-client", flag.ExitOnError)
	service := fs.String("service", "", "service name, e.g. exchange")
	grace := fs.Duration("grace", 24*time.Hour, "how long old key is accepted")
	_ = fs.Parse(args)

	ctx := context.Background()
	a := newAdmin(ctx)
	defer a.pool.Close()

	srv := serviceclient.NewService(a.log, serviceclient.NewRepository(), a.txm)

	key, client, old, err := srv.RotateClient(ctx, *service, *grace)
	if err != nil {
		a.log.Fatal().Err(err).Msg("failed to rotate service client")
	}

	fmt.Printf("service:            %s\n", client.Service)
	fmt.Printf("service_client_id:  %s\n", client.ID)
	fmt.Printf("X-Service-Key:      %s\n", key)
	fmt.Printf("old key %s works until %s UTC\n", old.ID, old.ExpiresAt.Format(time.DateTime))
	fmt.Println("save the key now: only its hash is stored")
}

func revokeServiceClient(args []string) {
	fs := flag.NewFlagSet("revoke-service-client", flag.ExitOnError)
	id := fs.String("id", "", "service client id")
	_ = fs.Parse(args)

	ctx := context.Background()
	a := newAdmin(ctx)
	defer a.pool.Close()

	clientID, err := uuid.Parse(*id)
	if err != nil {
		a.log.Fatal().Str("id", *id).Msg("invalid service client id")
	}

	srv := serviceclient.NewService(a.log, serviceclient.NewRepository(), a.txm)
	if err := srv.RevokeClient(ctx, clientID); err != nil {
		a.log.Fatal().Err(err).Msg("failed to revoke service client")
	}

	fmt.Printf("service client %s revoked\n", clientID)
}

func listServiceClients(args []string) {
	fs := flag.NewFlagSet("list-service-clients", flag.ExitOnError)
	service := fs.String("service", "", "service name, all services when empty")
	_ = fs.Parse(args)

	ctx := context.Background()
	a := newAdmin(ctx)
	defer a.pool.Close()

	srv := serviceclient.NewService(a.log, serviceclient.NewRepository(), a.txm)
	clients, err := srv.ListClients(ctx, *service)
	if err != nil {
		a.log.Fatal().Err(err).Msg("failed to list service clients")
	}

	now := time.Now().UTC()
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "SERVICE_CLIENT_ID\tSERVICE\tSTATE\tEXPIRES_AT\tREPLACED_BY\tCREATED_AT")
	for _, client := range clients {
		state, expiresAt, replacedBy := client.State(now), "", ""
		if client.ExpiresAt != nil {
			expiresAt = client.ExpiresAt.Format(time.DateTime)
		}
		if client.ReplacedBy != nil {
			replacedBy = client.ReplacedBy.String()
		}

		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n", client.ID, client.Service, state, expiresAt, replacedBy, client.CreatedAt.Format(time.DateTime))
	}
	_ = w.Flush()
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

func listRoles(args []string) {
	fs := flag.NewFlagSet("list-roles", flag.ExitOnError)
	service := fs.String("service", "", "service name, all services when empty")
	_ = fs.Parse(args)

	ctx := context.Background()
	a := newAdmin(ctx)
	defer a.pool.Close()

	roles, err := a.keyStorage().ListRoles(ctx, *service)
	if err != nil {
		a.log.Fatal().Err(err).Msg("failed to list roles")
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ROLE_ID\tSERVICE\tPURPOSE\tLABEL\tNETWORK\tKIND\tPOLICY")
	for _, role := range roles {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n", role.ID, role.Service, role.Purpose, role.Label, role.Network, role.Kind, role.Policy)
	}
	_ = w.Flush()
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

func listKeys(args []string) {
	fs := flag.NewFlagSet("list-keys", flag.ExitOnError)
	service := fs.String("service", "", "service name")
	purpose := fs.String("purpose", "", "role purpose")
	label := fs.String("label", "", "purpose refinement")
	networkName := fs.String("network", "", "ethereum, tron, arbitrum")
	statusName := fs.String("status", "", "active, rotating, pending, disabled, revoked")
	externalRef := fs.String("external-ref", "", "order id of deposit key")
	limit := fs.Int("limit", 100, "max keys, up to 1000")
	_ = fs.Parse(args)

	ctx := context.Background()
	a := newAdmin(ctx)
	defer a.pool.Close()

	filter := keystorage.ListKeysFilter{Limit: *limit}
	fs.Visit(func(f *flag.Flag) {
		switch f.Name {
		case "service":
			filter.Service = service
		case "purpose":
			filter.Purpose = purpose
		case "label":
			filter.Label = label
		case "external-ref":
			filter.ExternalRef = externalRef
		}
	})

	if *networkName != "" {
		network, ok := constants.ParseNetwork(*networkName)
		if !ok {
			a.log.Fatal().Str("network", *networkName).Msg("unsupported network")
		}
		filter.Network = &network
	}

	if *statusName != "" {
		status, ok := keystorage.ParseKeyStatus(*statusName)
		if !ok {
			a.log.Fatal().Str("status", *statusName).Msg("unsupported status")
		}
		filter.Status = &status
	}

	keys, err := a.keyStorage().ListKeys(ctx, filter)
	if err != nil {
		a.log.Fatal().Err(err).Msg("failed to list keys")
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "KEY_ID\tSERVICE\tPURPOSE\tLABEL\tNETWORK\tKIND\tADDRESS\tSTATUS\tREPLACED_BY\tLEASED_BY\tEXTERNAL_REF\tCREATED_AT")
	for _, key := range keys {
		externalRef := ""
		if key.ExternalRef != nil {
			externalRef = *key.ExternalRef
		}

		replacedBy := ""
		if key.ReplacedBy != nil {
			replacedBy = key.ReplacedBy.String()
		}

		leasedBy := ""
		if key.LeaseOwner != nil && key.LeasedUntil != nil && key.LeasedUntil.After(time.Now().UTC()) {
			leasedBy = *key.LeaseOwner + " until " + key.LeasedUntil.Format(time.TimeOnly)
		}

		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			key.ID, key.Role.Service, key.Role.Purpose, key.Role.Label, key.Network, key.Role.Kind,
			key.Address, key.Status, replacedBy, leasedBy, externalRef, key.CreatedAt.Format(time.DateTime))
	}
	_ = w.Flush()
}

func releaseLease(args []string) {
	fs := flag.NewFlagSet("release-lease", flag.ExitOnError)
	id := fs.String("id", "", "key id")
	_ = fs.Parse(args)

	ctx := context.Background()
	a := newAdmin(ctx)
	defer a.pool.Close()

	keyID, err := uuid.Parse(*id)
	if err != nil {
		a.log.Fatal().Str("id", *id).Msg("invalid key id")
	}

	if err := a.keyStorage().ForceReleaseLease(ctx, keyID); err != nil {
		a.log.Fatal().Err(err).Msg("failed to release lease")
	}

	fmt.Printf("lease of key %s released\n", keyID)
}

func keyAction(command string, args []string) {
	fs := flag.NewFlagSet(command, flag.ExitOnError)
	id := fs.String("id", "", "key id")
	_ = fs.Parse(args)

	ctx := context.Background()
	a := newAdmin(ctx)
	defer a.pool.Close()

	keyID, err := uuid.Parse(*id)
	if err != nil {
		a.log.Fatal().Str("id", *id).Msg("invalid key id")
	}

	srv := a.keyStorage()
	do := map[string]func(context.Context, uuid.UUID) (*keystorage.WalletKey, error){
		"rotate-key":   srv.RotateKey,
		"activate-key": srv.ActivateKey,
		"revoke-key":   srv.RevokeKey,
		"disable-key":  srv.DisableKey,
		"enable-key":   srv.EnableKey,
	}[command]

	key, err := do(ctx, keyID)
	if err != nil {
		a.log.Fatal().Err(err).Msg("failed to " + command)
	}

	fmt.Printf("key_id:   %s\n", key.ID)
	fmt.Printf("address:  %s\n", key.Address)
	fmt.Printf("status:   %s\n", key.Status)
	if command == "rotate-key" {
		fmt.Printf("replaces: %s, it may send its funds to this address; then activate-key -id %s\n", keyID, key.ID)
	}
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
		fmt.Fprintln(os.Stderr, "enter private key (hex) and press Enter:")
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
