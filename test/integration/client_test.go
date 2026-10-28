//go:build integration

package integration

import (
	"errors"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"

	"gitlab.tve-development.com/backend/gtoken/wallet-service/internal/constants"
	"gitlab.tve-development.com/backend/gtoken/wallet-service/pkg/client"
)

func TestClient(t *testing.T) {
	e := newEnv(t)
	gas := e.key(e.role("gas_station", constants.NetworkEthereum, constants.RoleKindSingle, `{"version":1,"rules":[{"op":"native","to":["internal:deposit"]}]}`), "")
	e.role("deposit", constants.NetworkEthereum, constants.RoleKindDeposit, "")
	e.fund(gas.Address, oneEth)
	c := client.New(e.apiURL, e.apiKey, nil)

	deposit, err := c.DepositKey(e.ctx, "deposit", client.DepositKeyRequest{Network: client.NetworkEthereum, ExternalRef: "order-1"})
	if err != nil || deposit.Kind != "deposit" || deposit.ExternalRef == nil || *deposit.ExternalRef != "order-1" {
		t.Fatalf("deposit key = %+v, %v", deposit, err)
	}
	if again, err := c.DepositKey(e.ctx, "deposit", client.DepositKeyRequest{Network: client.NetworkEthereum, ExternalRef: "order-1"}); err != nil || again.ID != deposit.ID {
		t.Fatalf("deposit key again = %+v, %v", again, err)
	}

	key, err := c.GetKey(e.ctx, "gas_station", "", client.NetworkEthereum)
	if err != nil || key.ID != gas.ID || key.Address != gas.Address || key.Status != client.StatusActive {
		t.Fatalf("get key = %+v, %v", key, err)
	}
	if keys, err := c.ListKeys(e.ctx, "gas_station", "", client.NetworkEthereum); err != nil || len(keys) != 1 || keys[0].ID != gas.ID {
		t.Fatalf("list keys = %+v, %v", keys, err)
	}

	lease, err := c.AcquireLease(e.ctx, "gas_station", client.LeaseRequest{Network: client.NetworkEthereum, Owner: "client-test"})
	if err != nil || lease.Key.ID != gas.ID {
		t.Fatalf("lease = %+v, %v", lease, err)
	}
	_, err = c.AcquireLease(e.ctx, "gas_station", client.LeaseRequest{Network: client.NetworkEthereum})
	var apiErr *client.Error
	if !errors.Is(err, client.ErrNoFreeKey) || !errors.As(err, &apiErr) || apiErr.RetryAfter == 0 {
		t.Fatalf("second lease: %v", err)
	}

	nonce, err := e.eth.PendingNonceAt(e.ctx, common.HexToAddress(gas.Address))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := e.unsignedTx(nonce, common.HexToAddress(deposit.Address), big.NewInt(1000), true).MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	req := client.SignRequest{Network: client.NetworkEthereum, RawTx: "0x" + common.Bytes2Hex(raw)}

	if _, err := c.SignTransaction(e.ctx, gas.ID, req); !errors.Is(err, client.ErrLeaseRequired) {
		t.Fatalf("sign without lease: %v", err)
	}

	req.LeaseID = &lease.LeaseID
	signed, err := c.SignTransaction(e.ctx, gas.ID, req)
	if err != nil {
		t.Fatal(err)
	}
	tx := new(types.Transaction)
	if err := tx.UnmarshalBinary(common.FromHex(signed.SignedTx)); err != nil || tx.Hash().Hex() != signed.TxHash {
		t.Fatalf("signed tx: hash %s, want %s, %v", tx.Hash().Hex(), signed.TxHash, err)
	}
	e.broadcast(tx)

	if expiresAt, err := c.ExtendLease(e.ctx, lease.LeaseID, 120); err != nil || !expiresAt.After(lease.ExpiresAt) {
		t.Fatalf("extend = %v, %v, was %v", expiresAt, err, lease.ExpiresAt)
	}
	if err := c.ReleaseLease(e.ctx, lease.LeaseID); err != nil {
		t.Fatal(err)
	}
	if err := c.ReleaseLease(e.ctx, lease.LeaseID); !errors.Is(err, client.ErrLeaseExpired) {
		t.Fatalf("release again: %v", err)
	}

	if _, err := c.GetKey(e.ctx, "unknown", "", client.NetworkEthereum); !errors.Is(err, client.ErrRoleNotFound) {
		t.Fatalf("unknown role: %v", err)
	}
	if _, err := client.New(e.apiURL, "wrong", nil).GetKey(e.ctx, "gas_station", "", client.NetworkEthereum); !errors.Is(err, client.ErrUnauthorized) {
		t.Fatalf("wrong service key: %v", err)
	}
}
