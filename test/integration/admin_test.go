//go:build integration

package integration

import (
	"math/big"
	"net/http"
	"testing"

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
