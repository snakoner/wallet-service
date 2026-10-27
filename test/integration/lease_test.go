//go:build integration

package integration

import (
	"math/big"
	"net/http"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/google/uuid"

	"gitlab.tve-development.com/backend/gtoken/wallet-service/internal/constants"
)

type leaseView struct {
	LeaseID uuid.UUID `json:"lease_id"`
	Key     keyView   `json:"key"`
}

func TestLeasePoolKey(t *testing.T) {
	e := newEnv(t)
	executor := e.key(e.role("executor", constants.NetworkEthereum, constants.RoleKindPool, `{"version":1,"rules":[{"op":"native","to":["internal:deposit"]}]}`), "")
	deposit := e.key(e.role("deposit", constants.NetworkEthereum, constants.RoleKindDeposit, ""), "order-1")
	e.fund(executor.Address, oneEth)

	var lease leaseView
	if status, res := e.call(http.MethodPost, "/api/v1/roles/executor/lease", e.apiKey, map[string]any{"network": "ethereum", "owner": "pod-1", "ttl_seconds": 60}, &lease); status != http.StatusOK || lease.Key.ID != executor.ID {
		t.Fatalf("lease: status = %d, lease = %+v, error = %+v", status, lease, res.Error)
	}

	if status, res := e.call(http.MethodPost, "/api/v1/roles/executor/lease", e.apiKey, map[string]any{"network": "ethereum"}, nil); status != http.StatusConflict || res.Error.Code != "NO_FREE_KEY" {
		t.Fatalf("second lease: status = %d, error = %+v", status, res.Error)
	}

	from := common.HexToAddress(executor.Address)
	nonce, err := e.eth.PendingNonceAt(e.ctx, from)
	if err != nil {
		t.Fatal(err)
	}

	raw, err := e.unsignedTx(nonce, common.HexToAddress(deposit.Address), big.NewInt(1000), true).MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}

	sign := func(leaseID *uuid.UUID) (int, apiResponse, signed) {
		var out signed
		body := map[string]any{"network": "ethereum", "raw_tx": "0x" + common.Bytes2Hex(raw)}
		if leaseID != nil {
			body["lease_id"] = leaseID.String()
		}
		status, res := e.call(http.MethodPost, "/api/v1/keys/"+executor.ID.String()+"/sign-transaction", e.apiKey, body, &out)
		return status, res, out
	}

	if status, res, _ := sign(nil); status != http.StatusConflict || res.Error.Code != "LEASE_REQUIRED" {
		t.Fatalf("pool key without lease: status = %d, error = %+v", status, res.Error)
	}

	status, res, out := sign(&lease.LeaseID)
	if status != http.StatusOK {
		t.Fatalf("sign with lease: status = %d, error = %+v", status, res.Error)
	}

	tx := new(types.Transaction)
	if err := tx.UnmarshalBinary(common.FromHex(out.SignedTx)); err != nil {
		t.Fatal(err)
	}

	e.broadcast(tx)

	if status, res := e.call(http.MethodPost, "/api/v1/leases/"+lease.LeaseID.String()+"/extend", e.apiKey, map[string]any{"ttl_seconds": 120}, nil); status != http.StatusOK {
		t.Fatalf("extend: status = %d, error = %+v", status, res.Error)
	}

	// other service can not touch the lease
	other := newEnv(t)
	if status, res := other.call(http.MethodDelete, "/api/v1/leases/"+lease.LeaseID.String(), other.apiKey, nil, nil); status != http.StatusConflict || res.Error.Code != "LEASE_EXPIRED" {
		t.Fatalf("release by other service: status = %d, error = %+v", status, res.Error)
	}

	if status, res := e.call(http.MethodDelete, "/api/v1/leases/"+lease.LeaseID.String(), e.apiKey, nil, nil); status != http.StatusOK {
		t.Fatalf("release: status = %d, error = %+v", status, res.Error)
	}

	if status, res, _ := sign(&lease.LeaseID); status != http.StatusConflict || res.Error.Code != "LEASE_EXPIRED" {
		t.Fatalf("sign with released lease: status = %d, error = %+v", status, res.Error)
	}

	// admin frees key held by stuck instance
	var stuck leaseView
	e.call(http.MethodPost, "/api/v1/roles/executor/lease", e.apiKey, map[string]any{"network": "ethereum", "ttl_seconds": 600}, &stuck)
	if status, res := e.call(http.MethodPost, "/api/v1/admin/keys/"+executor.ID.String()+"/release-lease", "", nil, nil); status != http.StatusOK {
		t.Fatalf("admin release: status = %d, error = %+v", status, res.Error)
	}

	if status, res := e.call(http.MethodPost, "/api/v1/roles/executor/lease", e.apiKey, map[string]any{"network": "ethereum"}, nil); status != http.StatusOK {
		t.Fatalf("lease after admin release: status = %d, error = %+v", status, res.Error)
	}
}

func TestLeaseSingleKey(t *testing.T) {
	e := newEnv(t)
	gas := e.key(e.role("gas_station", constants.NetworkEthereum, constants.RoleKindSingle, gasStationPolicy), "")
	deposit := e.key(e.role("deposit", constants.NetworkEthereum, constants.RoleKindDeposit, ""), "order-1")
	raw, err := e.unsignedTx(0, common.HexToAddress(deposit.Address), big.NewInt(1), true).MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}

	if status, res, _ := e.sign(gas.ID, "ethereum", raw); status != http.StatusOK {
		t.Fatalf("single key without lease: status = %d, error = %+v", status, res.Error)
	}

	var lease leaseView
	if status, res := e.call(http.MethodPost, "/api/v1/roles/gas_station/lease", e.apiKey, map[string]any{"network": "ethereum"}, &lease); status != http.StatusOK {
		t.Fatalf("lease single: status = %d, error = %+v", status, res.Error)
	}

	if status, res, _ := e.sign(gas.ID, "ethereum", raw); status != http.StatusConflict || res.Error.Code != "KEY_LEASED" {
		t.Fatalf("leased single key without lease: status = %d, error = %+v", status, res.Error)
	}
}
