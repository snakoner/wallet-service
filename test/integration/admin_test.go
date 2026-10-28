//go:build integration

package integration

import (
	"encoding/json"
	"math/big"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/google/uuid"

	"gitlab.tve-development.com/backend/gtoken/wallet-service/internal/constants"
)

type issuedKey struct {
	ServiceKey string `json:"service_key"`
	Client     struct {
		ID    uuid.UUID `json:"id"`
		State string    `json:"state"`
	} `json:"client"`
	OldClient *struct {
		ID    uuid.UUID `json:"id"`
		State string    `json:"state"`
	} `json:"old_client"`
}

func TestAdminServiceClients(t *testing.T) {
	e := newEnv(t)
	service := "it-" + uuid.NewString()[:8]
	keyCheck := func(key string) int {
		status, _ := e.call(http.MethodGet, "/api/v1/roles/gas_station/key?network=ethereum", key, nil, nil)
		return status
	}

	var first issuedKey
	if status, res := e.call(http.MethodPost, "/api/v1/admin/service-clients", "", map[string]string{"service": service}, &first); status != http.StatusOK || first.ServiceKey == "" || first.Client.State != "primary" {
		t.Fatalf("create: status = %d, issued = %+v, error = %+v", status, first, res.Error)
	}

	// authorized key without role gives 404, not 401
	if status := keyCheck(first.ServiceKey); status != http.StatusNotFound {
		t.Fatalf("first key: status = %d", status)
	}

	if status, res := e.call(http.MethodPost, "/api/v1/admin/service-clients", "", map[string]string{"service": service}, nil); status != http.StatusConflict || res.Error.Code != "SERVICE_CLIENT_EXISTS" {
		t.Fatalf("second create: status = %d, error = %+v", status, res.Error)
	}

	var second issuedKey
	if status, res := e.call(http.MethodPost, "/api/v1/admin/service-clients/rotate", "", map[string]any{"service": service, "grace_seconds": 3600}, &second); status != http.StatusOK || second.OldClient == nil || second.OldClient.State != "expiring" {
		t.Fatalf("rotate: status = %d, issued = %+v, error = %+v", status, second, res.Error)
	}

	if keyCheck(first.ServiceKey) != http.StatusNotFound || keyCheck(second.ServiceKey) != http.StatusNotFound {
		t.Fatal("both keys must work during grace period")
	}

	var third issuedKey
	e.call(http.MethodPost, "/api/v1/admin/service-clients/rotate", "", map[string]any{"service": service, "grace_seconds": 0}, &third)
	if status := keyCheck(second.ServiceKey); status != http.StatusUnauthorized {
		t.Fatalf("key rotated with grace 0: status = %d", status)
	}

	if status, res := e.call(http.MethodPost, "/api/v1/admin/service-clients/"+third.Client.ID.String()+"/revoke", "", nil, nil); status != http.StatusOK {
		t.Fatalf("revoke: status = %d, error = %+v", status, res.Error)
	}

	if status := keyCheck(third.ServiceKey); status != http.StatusUnauthorized {
		t.Fatalf("revoked key: status = %d", status)
	}

	var clients []struct {
		State string `json:"state"`
	}
	e.call(http.MethodGet, "/api/v1/admin/service-clients?service="+service, "", nil, &clients)
	states := map[string]int{}
	for _, c := range clients {
		states[c.State]++
	}

	if len(clients) != 3 || states["expiring"] != 1 || states["expired"] != 1 || states["revoked"] != 1 {
		t.Fatalf("clients = %+v", clients)
	}
}

func TestAdminImportKey(t *testing.T) {
	e := newEnv(t)
	role := e.role("gas_station", constants.NetworkEthereum, constants.RoleKindSingle, gasStationPolicy)
	deposit := e.key(e.role("deposit", constants.NetworkEthereum, constants.RoleKindDeposit, ""), "order-1")

	privateKey, err := crypto.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}

	address := crypto.PubkeyToAddress(privateKey.PublicKey)

	var key keyView
	status, res := e.call(http.MethodPost, "/api/v1/admin/roles/"+role.ID.String()+"/keys/import", "",
		map[string]string{"private_key": "0x" + common.Bytes2Hex(crypto.FromECDSA(privateKey)), "alias": "migrated from env"}, &key)
	if status != http.StatusOK || key.Address != address.Hex() || key.Status != "active" {
		t.Fatalf("import: status = %d, key = %+v, error = %+v", status, key, res.Error)
	}

	// imported key signs: sender of signed tx is imported address
	e.fund(address.Hex(), oneEth)
	nonce, _ := e.eth.PendingNonceAt(e.ctx, address)
	raw, _ := e.unsignedTx(nonce, common.HexToAddress(deposit.Address), big.NewInt(1), true).MarshalBinary()
	status, res, out := e.sign(key.ID, "ethereum", raw)
	if status != http.StatusOK {
		t.Fatalf("sign: status = %d, error = %+v", status, res.Error)
	}

	tx := new(types.Transaction)
	if err := tx.UnmarshalBinary(common.FromHex(out.SignedTx)); err != nil {
		t.Fatal(err)
	}

	e.broadcast(tx)
	if sender, err := types.Sender(types.LatestSignerForChainID(anvilChainID), tx); err != nil || sender != address {
		t.Fatalf("sender = %s, err = %v", sender, err)
	}

	if status, res := e.call(http.MethodPost, "/api/v1/admin/roles/"+role.ID.String()+"/keys/import", "",
		map[string]string{"private_key": common.Bytes2Hex(crypto.FromECDSA(privateKey))}, nil); status != http.StatusConflict {
		t.Fatalf("same key twice: status = %d, error = %+v", status, res.Error)
	}
}

func TestAdminKeyAndRoleCards(t *testing.T) {
	e := newEnv(t)
	gas := e.key(e.role("gas_station", constants.NetworkEthereum, constants.RoleKindSingle, gasStationPolicy), "")
	depositRole := e.role("deposit", constants.NetworkEthereum, constants.RoleKindDeposit, "")
	for _, ref := range []string{"order-1", "order-2", "order-3"} {
		e.key(depositRole, ref)
	}
	leaseID := e.lease(gas.ID)

	var card struct {
		ID          uuid.UUID       `json:"id"`
		KEKVersion  int32           `json:"kek_version"`
		LeaseID     *uuid.UUID      `json:"lease_id"`
		LeaseActive bool            `json:"lease_active"`
		Policy      json.RawMessage `json:"policy"`
		UpdatedAt   time.Time       `json:"updated_at"`
	}
	if status, res := e.call(http.MethodGet, "/api/v1/admin/keys/"+gas.ID.String(), "", nil, &card); status != http.StatusOK || card.ID != gas.ID ||
		card.KEKVersion != gas.KEKVersion || !card.LeaseActive || card.LeaseID == nil || *card.LeaseID != *leaseID || len(card.Policy) < 3 || card.UpdatedAt.IsZero() {
		t.Fatalf("key card: status = %d, error = %+v, card = %+v", status, res.Error, card)
	}
	if status, res := e.call(http.MethodGet, "/api/v1/admin/keys/"+uuid.NewString(), "", nil, nil); status != http.StatusNotFound || res.Error.Code != "KEY_NOT_FOUND" {
		t.Fatalf("unknown key: status = %d, error = %+v", status, res.Error)
	}

	var role struct {
		ID        uuid.UUID      `json:"id"`
		KeyCounts map[string]int `json:"key_counts"`
	}
	if status, res := e.call(http.MethodGet, "/api/v1/admin/roles/"+depositRole.ID.String(), "", nil, &role); status != http.StatusOK || role.ID != depositRole.ID ||
		len(role.KeyCounts) != 5 || role.KeyCounts["active"] != 3 || role.KeyCounts["revoked"] != 0 {
		t.Fatalf("role card: status = %d, error = %+v, role = %+v", status, res.Error, role)
	}
	if status, _ := e.call(http.MethodGet, "/api/v1/admin/roles/"+uuid.NewString(), "", nil, nil); status != http.StatusNotFound {
		t.Fatalf("unknown role: status = %d", status)
	}

	type keyView struct {
		ID uuid.UUID `json:"id"`
	}
	seen := map[uuid.UUID]bool{}
	type keysPage struct {
		Keys  []keyView `json:"keys"`
		Total int64     `json:"total"`
	}
	for _, page := range []struct {
		query string
		want  int
	}{{"?limit=2", 2}, {"?offset=2&limit=2", 1}, {"?offset=5", 0}} {
		var keys keysPage
		if status, res := e.call(http.MethodGet, "/api/v1/admin/roles/"+depositRole.ID.String()+"/keys"+page.query, "", nil, &keys); status != http.StatusOK || len(keys.Keys) != page.want || keys.Total != 3 {
			t.Fatalf("role keys %s: status = %d, error = %+v, keys = %d, total = %d", page.query, status, res.Error, len(keys.Keys), keys.Total)
		}
		for _, key := range keys.Keys {
			seen[key.ID] = true
		}
	}
	if len(seen) != 3 {
		t.Fatalf("role key pages overlap: %d distinct keys, want 3", len(seen))
	}

	for _, query := range []string{"?address=" + strings.ToLower(gas.Address), "?service=" + e.service + "&leased=true"} {
		var keys keysPage
		if status, res := e.call(http.MethodGet, "/api/v1/admin/keys"+query, "", nil, &keys); status != http.StatusOK || len(keys.Keys) != 1 || keys.Total != 1 || keys.Keys[0].ID != gas.ID {
			t.Fatalf("keys %s: status = %d, error = %+v, keys = %+v", query, status, res.Error, keys)
		}
	}
}
