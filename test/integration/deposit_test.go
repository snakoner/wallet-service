//go:build integration

package integration

import (
	"math/big"
	"net/http"
	"sync"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/google/uuid"

	"gitlab.tve-development.com/backend/gtoken/wallet-service/internal/constants"
)

type depositKeyView struct {
	ID          uuid.UUID `json:"id"`
	Address     string    `json:"address"`
	ExternalRef string    `json:"external_ref"`
	Kind        string    `json:"kind"`
}

func (e *env) depositKey(externalRef string) (int, apiResponse, depositKeyView) {
	e.tb.Helper()
	var key depositKeyView
	status, res := e.call(http.MethodPost, "/api/v1/roles/deposit/deposit-key", e.apiKey, map[string]any{"network": "ethereum", "external_ref": externalRef}, &key)
	return status, res, key
}

func TestDepositKey(t *testing.T) {
	e := newEnv(t)
	gas := e.key(e.role("gas_station", constants.NetworkEthereum, constants.RoleKindSingle, ""), "")
	e.role("deposit", constants.NetworkEthereum, constants.RoleKindDeposit, `{"version":1,"rules":[{"op":"native","to":["internal:gas_station"]}]}`)

	status, res, key := e.depositKey("order-1")
	if status != http.StatusOK || key.ExternalRef != "order-1" || key.Kind != "deposit" || key.Address == "" {
		t.Fatalf("create: status = %d, key = %+v, error = %+v", status, key, res.Error)
	}

	if status, res, again := e.depositKey("order-1"); status != http.StatusOK || again.ID != key.ID {
		t.Fatalf("repeat: status = %d, key = %+v, error = %+v", status, again, res.Error)
	}

	var wg sync.WaitGroup
	ids := make([]uuid.UUID, 10)
	for i := range ids {
		wg.Go(func() {
			if status, res, k := e.depositKey("order-2"); status == http.StatusOK {
				ids[i] = k.ID
			} else {
				t.Errorf("concurrent: status = %d, error = %+v", status, res.Error)
			}
		})
	}
	wg.Wait()
	for _, id := range ids {
		if id != ids[0] || id == key.ID {
			t.Fatalf("concurrent calls returned different keys: %v", ids)
		}
	}

	if status, res := e.call(http.MethodPost, "/api/v1/roles/gas_station/deposit-key", e.apiKey, map[string]any{"network": "ethereum", "external_ref": "order-3"}, nil); status != http.StatusBadRequest || res.Error.Code != "INVALID_PARAMS" {
		t.Fatalf("not deposit role: status = %d, error = %+v", status, res.Error)
	}

	other := newEnv(t)
	if status, res, _ := other.depositKey("order-1"); status != http.StatusNotFound || res.Error.Code != "ROLE_NOT_FOUND" {
		t.Fatalf("other service: status = %d, error = %+v", status, res.Error)
	}

	from := common.HexToAddress(key.Address)
	e.fund(key.Address, oneEth)
	if status, res := e.signAndSend(key.ID, from, common.HexToAddress(gas.Address), big.NewInt(1000)); status != http.StatusOK {
		t.Fatalf("sweep to gas station: status = %d, error = %+v", status, res.Error)
	}

	if status, res := e.signAndSend(key.ID, from, common.HexToAddress("0x4444444444444444444444444444444444444444"), big.NewInt(1000)); status != http.StatusForbidden {
		t.Fatalf("sweep to stranger: status = %d, error = %+v", status, res.Error)
	}
}
