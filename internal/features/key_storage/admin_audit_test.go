package key_storage

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	"gitlab.tve-development.com/backend/gtoken/wallet-service/internal/constants"
	adminaudit "gitlab.tve-development.com/backend/gtoken/wallet-service/internal/features/admin_audit"
)

func lastRecord(t *testing.T, audit *adminAuditStub, action string) adminaudit.Record {
	t.Helper()
	if len(audit.records) == 0 {
		t.Fatalf("no records, want %s", action)
	}
	record := audit.records[len(audit.records)-1]
	if record.Action != action || record.Service != "exchange" || record.RoleID == nil {
		t.Fatalf("record = %+v, want %s of exchange role", record, action)
	}
	return record
}

func TestAdminAuditOfKeys(t *testing.T) {
	ctx := context.Background()
	repo := &repoStub{}
	srv, audit := newAuditedTestService(t, repo)

	role := createTestRole(t, srv, constants.NetworkEthereum, constants.RoleKindSingle)
	record := lastRecord(t, audit, adminaudit.ActionRoleCreate)
	if *record.RoleID != role.ID || record.KeyID != nil || record.Details["kind"] != "single" || record.Details["network"] != "ethereum" {
		t.Fatalf("role.create = %+v", record)
	}

	oldPolicy := role.Policy
	newPolicy := `{"version":1,"rules":[{"op":"native","to":["*"]}]}`
	if _, err := srv.UpdatePolicy(ctx, role.ID, json.RawMessage(newPolicy)); err != nil {
		t.Fatal(err)
	}
	record = lastRecord(t, audit, adminaudit.ActionRolePolicyUpdate)
	if string(record.Details["old"].(json.RawMessage)) != string(oldPolicy) || !strings.Contains(string(record.Details["new"].(json.RawMessage)), `"native"`) {
		t.Fatalf("role.policy_update = %+v", record.Details)
	}

	key, err := srv.CreateKey(ctx, CreateKeyParams{RoleID: role.ID})
	if err != nil {
		t.Fatal(err)
	}
	record = lastRecord(t, audit, adminaudit.ActionKeyCreate)
	if *record.KeyID != key.ID || record.Details["address"] != key.Address {
		t.Fatalf("key.create = %+v", record)
	}

	replacement, err := srv.RotateKey(ctx, key.ID)
	if err != nil {
		t.Fatal(err)
	}
	record = lastRecord(t, audit, adminaudit.ActionKeyRotate)
	if *record.KeyID != key.ID || record.Details["replacement_key_id"] != replacement.ID || record.Details["replacement_address"] != replacement.Address {
		t.Fatalf("key.rotate = %+v", record)
	}

	if _, err := srv.ActivateKey(ctx, replacement.ID); err != nil {
		t.Fatal(err)
	}
	record = lastRecord(t, audit, adminaudit.ActionKeyActivate)
	if *record.KeyID != replacement.ID || record.Details["replaced_key_id"] != key.ID || record.Details["replaced_key_status"] != "rotating" || record.Details["from"] != "pending" {
		t.Fatalf("key.activate = %+v", record)
	}

	if _, err := srv.DisableKey(ctx, replacement.ID); err != nil {
		t.Fatal(err)
	}
	record = lastRecord(t, audit, adminaudit.ActionKeyDisable)
	if record.Details["from"] != "active" || record.Details["to"] != "disabled" {
		t.Fatalf("key.disable = %+v", record)
	}

	if _, err := srv.EnableKey(ctx, replacement.ID); err != nil {
		t.Fatal(err)
	}
	lastRecord(t, audit, adminaudit.ActionKeyEnable)

	// revoking pending replacement cancels rotation of active key
	pending, err := srv.RotateKey(ctx, replacement.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := srv.RevokeKey(ctx, pending.ID); err != nil {
		t.Fatal(err)
	}
	record = lastRecord(t, audit, adminaudit.ActionKeyRevoke)
	if *record.KeyID != pending.ID || record.Details["from"] != "pending" || record.Details["replaced_key_id"] != replacement.ID {
		t.Fatalf("key.revoke = %+v", record)
	}

	if err := srv.ForceReleaseLease(ctx, replacement.ID); err != nil {
		t.Fatal(err)
	}
	record = lastRecord(t, audit, adminaudit.ActionKeyReleaseLease)
	if *record.KeyID != replacement.ID {
		t.Fatalf("key.release_lease = %+v", record)
	}

	if err := srv.ForceReleaseLease(ctx, uuid.New()); !errors.Is(err, ErrKeyNotFound) {
		t.Fatalf("release lease of unknown key: err = %v", err)
	}

	// failed actions are not recorded
	count := len(audit.records)
	if _, err := srv.ActivateKey(ctx, replacement.ID); !errors.Is(err, ErrInvalidKeyStatus) || len(audit.records) != count {
		t.Fatalf("failed activate: err = %v, records %d -> %d", err, count, len(audit.records))
	}
}

func TestAdminAuditOfImportAndDeposit(t *testing.T) {
	ctx := context.Background()
	repo := &repoStub{}
	srv, audit := newAuditedTestService(t, repo)
	pool := createTestRole(t, srv, constants.NetworkEthereum, constants.RoleKindPool)

	privateKey, address, err := keysByNetwork[constants.NetworkEthereum].generate()
	if err != nil {
		t.Fatal(err)
	}
	key, err := srv.ImportKey(ctx, ImportKeyParams{RoleID: pool.ID, PrivateKey: []byte(privateKey)})
	if err != nil {
		t.Fatal(err)
	}
	record := lastRecord(t, audit, adminaudit.ActionKeyImport)
	details, _ := json.Marshal(record.Details)
	if *record.KeyID != key.ID || record.Details["address"] != address || strings.Contains(string(details), strings.TrimPrefix(privateKey, "0x")) {
		t.Fatalf("key.import = %s", details)
	}

	// key created by service itself is not admin action
	deposit, err := srv.CreateRole(ctx, CreateRoleParams{Service: "exchange", Purpose: "deposit", Network: constants.NetworkEthereum, Kind: constants.RoleKindDeposit})
	if err != nil {
		t.Fatal(err)
	}
	count := len(audit.records)
	if _, err := srv.GetOrCreateDepositKey(ctx, GetDepositKeyParams{Service: "exchange", Purpose: deposit.Purpose, Network: deposit.Network, ExternalRef: "order-1"}); err != nil {
		t.Fatal(err)
	}
	if len(audit.records) != count {
		t.Fatalf("deposit key by service recorded: %+v", audit.records[count:])
	}
}

func TestAdminAuditFailureFailsAction(t *testing.T) {
	ctx := context.Background()
	repo := &repoStub{}
	srv, audit := newAuditedTestService(t, repo)
	role := createTestRole(t, srv, constants.NetworkEthereum, constants.RoleKindPool)

	audit.err = errors.New("audit is down")
	if _, err := srv.CreateKey(ctx, CreateKeyParams{RoleID: role.ID}); !errors.Is(err, audit.err) {
		t.Fatalf("create key: err = %v", err)
	}
	if _, err := srv.UpdatePolicy(ctx, role.ID, json.RawMessage(`{}`)); !errors.Is(err, audit.err) {
		t.Fatalf("update policy: err = %v", err)
	}
}

func TestRewrapKeysNeedsActor(t *testing.T) {
	repo := &repoStub{}
	srv, audit := newAuditedTestService(t, repo)
	role := createTestRole(t, srv, constants.NetworkEthereum, constants.RoleKindPool)
	if _, err := srv.CreateKey(context.Background(), CreateKeyParams{RoleID: role.ID}); err != nil {
		t.Fatal(err)
	}
	count := len(audit.records)

	if _, _, err := srv.RewrapKeys(context.Background()); !errors.Is(err, adminaudit.ErrNoActor) || len(audit.records) != count {
		t.Fatalf("rewrap without actor: err = %v", err)
	}

	ctx := adminaudit.WithActor(context.Background(), "ivanov", adminaudit.SourceCLI)
	if _, _, err := srv.RewrapKeys(ctx); err != nil {
		t.Fatal(err)
	}
	record := audit.records[len(audit.records)-1]
	if record.Action != adminaudit.ActionKEKRewrap || record.Details["kek_version"] != int32(1) || record.Details["rewrapped"] != 0 || record.Details["left"] != int64(0) {
		t.Fatalf("kek.rewrap = %+v", record)
	}
}
