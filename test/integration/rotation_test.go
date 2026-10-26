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
	keystorage "gitlab.tve-development.com/backend/gtoken/wallet-service/internal/features/key_storage"
)

type keyView struct {
	ID         uuid.UUID  `json:"id"`
	Address    string     `json:"address"`
	Status     string     `json:"status"`
	ReplacedBy *uuid.UUID `json:"replaced_by"`
}

func (e *env) signAndSend(keyID uuid.UUID, from, to common.Address, value *big.Int) (int, apiResponse) {
	e.tb.Helper()
	nonce, err := e.eth.PendingNonceAt(e.ctx, from)
	if err != nil {
		e.tb.Fatal(err)
	}
	raw, err := e.unsignedTx(nonce, to, value, false).MarshalBinary()
	if err != nil {
		e.tb.Fatal(err)
	}

	status, res, out := e.sign(keyID, "ethereum", raw)
	if status != http.StatusOK {
		return status, res
	}

	tx := new(types.Transaction)
	if err := tx.UnmarshalBinary(common.FromHex(out.SignedTx)); err != nil {
		e.tb.Fatal(err)
	}
	e.broadcast(tx)

	return status, res
}

func (e *env) admin(action string, keyID uuid.UUID) keyView {
	e.tb.Helper()
	var key keyView
	if status, res := e.call(http.MethodPost, "/api/v1/admin/keys/"+keyID.String()+"/"+action, "", nil, &key); status != http.StatusOK {
		e.tb.Fatalf("%s %s: status = %d, error = %+v", action, keyID, status, res.Error)
	}
	return key
}

func TestRotateGasStation(t *testing.T) {
	e := newEnv(t)
	old := e.key(e.role("gas_station", constants.NetworkEthereum, keystorage.RoleKindSingle, gasStationPolicy), "")
	deposit := e.key(e.role("deposit", constants.NetworkEthereum, keystorage.RoleKindDeposit, ""), "order-1")
	e.fund(old.Address, oneEth)
	oldAddress := common.HexToAddress(old.Address)
	halfEth := new(big.Int).Div(oneEth, big.NewInt(2))

	replacement := e.admin("rotate", old.ID)
	if replacement.Status != "pending" {
		t.Fatalf("replacement = %+v", replacement)
	}
	replacementAddress := common.HexToAddress(replacement.Address)

	var current keyView
	e.call(http.MethodGet, "/api/v1/roles/gas_station/key?network=ethereum", e.apiKey, nil, &current)
	if current.ID != old.ID || current.ReplacedBy == nil || *current.ReplacedBy != replacement.ID {
		t.Fatalf("current before activation = %+v", current)
	}

	if status, res := e.signAndSend(old.ID, oldAddress, replacementAddress, halfEth); status != http.StatusOK {
		t.Fatalf("fund replacement: status = %d, error = %+v", status, res.Error)
	}
	if status, res := e.signAndSend(old.ID, oldAddress, common.HexToAddress("0x4444444444444444444444444444444444444444"), big.NewInt(1)); status != http.StatusForbidden || res.Error.Code != "POLICY_VIOLATION" {
		t.Fatalf("old key to stranger: status = %d, error = %+v", status, res.Error)
	}

	e.admin("activate", replacement.ID)
	e.call(http.MethodGet, "/api/v1/roles/gas_station/key?network=ethereum", e.apiKey, nil, &current)
	if current.ID != replacement.ID {
		t.Fatalf("current after activation = %+v", current)
	}
	if status, res := e.signAndSend(replacement.ID, replacementAddress, common.HexToAddress(deposit.Address), big.NewInt(1)); status != http.StatusOK {
		t.Fatalf("replacement funds deposit: status = %d, error = %+v", status, res.Error)
	}

	balance, err := e.eth.BalanceAt(e.ctx, oldAddress, nil)
	if err != nil {
		t.Fatal(err)
	}
	gasPrice, err := e.eth.SuggestGasPrice(e.ctx)
	if err != nil {
		t.Fatal(err)
	}
	rest := new(big.Int).Sub(balance, new(big.Int).Mul(gasPrice, big.NewInt(21000)))
	nonce, _ := e.eth.PendingNonceAt(e.ctx, oldAddress)
	raw, _ := types.NewTransaction(nonce, replacementAddress, rest, 21000, gasPrice, nil).MarshalBinary()
	status, res, out := e.sign(old.ID, "ethereum", raw)
	if status != http.StatusOK {
		t.Fatalf("sweep: status = %d, error = %+v", status, res.Error)
	}
	sweep := new(types.Transaction)
	if err := sweep.UnmarshalBinary(common.FromHex(out.SignedTx)); err != nil {
		t.Fatal(err)
	}
	e.broadcast(sweep)
	if left, _ := e.eth.BalanceAt(e.ctx, oldAddress, nil); left.Sign() != 0 {
		t.Fatalf("old key balance after sweep = %s", left)
	}

	if revoked := e.admin("revoke", old.ID); revoked.Status != "revoked" {
		t.Fatalf("revoked = %+v", revoked)
	}
	if status, res, _ := e.sign(old.ID, "ethereum", raw); status != http.StatusConflict || res.Error.Code != "KEY_NOT_ACTIVE" {
		t.Fatalf("revoked key: status = %d, error = %+v", status, res.Error)
	}
}

func TestCancelRotation(t *testing.T) {
	e := newEnv(t)
	old := e.key(e.role("gas_station", constants.NetworkEthereum, keystorage.RoleKindSingle, gasStationPolicy), "")

	replacement := e.admin("rotate", old.ID)
	if status, res := e.call(http.MethodPost, "/api/v1/admin/keys/"+old.ID.String()+"/rotate", "", nil, nil); status != http.StatusConflict || res.Error.Code != "ROTATION_IN_PROGRESS" {
		t.Fatalf("second rotation: status = %d, error = %+v", status, res.Error)
	}

	e.admin("revoke", replacement.ID)
	if again := e.admin("rotate", old.ID); again.Status != "pending" {
		t.Fatalf("rotation after cancel = %+v", again)
	}
}
