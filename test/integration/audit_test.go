//go:build integration

package integration

import (
	"bytes"
	"math/big"
	"net/http"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/google/uuid"

	"gitlab.tve-development.com/backend/gtoken/wallet-service/internal/constants"
)

func TestSigningAudit(t *testing.T) {
	e := newEnv(t)
	gas := e.key(e.role("gas_station", constants.NetworkEthereum, constants.RoleKindSingle, gasStationPolicy), "")
	deposit := e.key(e.role("deposit", constants.NetworkEthereum, constants.RoleKindDeposit, ""), "order-1")

	allowed, err := e.unsignedTx(0, common.HexToAddress(deposit.Address), big.NewInt(1), true).MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}

	denied, err := e.unsignedTx(1, common.HexToAddress("0x4444444444444444444444444444444444444444"), big.NewInt(1), true).MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}

	status, res, out := e.sign(gas.ID, "ethereum", allowed)
	if status != http.StatusOK {
		t.Fatalf("sign: status = %d, error = %+v", status, res.Error)
	}

	if status, _, _ := e.sign(gas.ID, "ethereum", denied); status != http.StatusForbidden {
		t.Fatalf("denied: status = %d", status)
	}

	unknownKey := uuid.New()
	if status, _, _ := e.sign(unknownKey, "ethereum", allowed); status != http.StatusNotFound {
		t.Fatalf("unknown key: status = %d", status)
	}

	var clientID uuid.UUID
	if err := e.pool.QueryRow(e.ctx, `SELECT id FROM service_clients WHERE service = $1`, e.service).Scan(&clientID); err != nil {
		t.Fatal(err)
	}

	type record struct {
		clientID  uuid.UUID
		network   constants.Network
		rawTx     []byte
		txHash    *string
		errorCode *string
	}
	records := func(keyID uuid.UUID) []record {
		rows, err := e.pool.Query(e.ctx, `
			SELECT service_client_id, network, raw_tx, tx_hash, error_code
			FROM signing_audit
			WHERE key_id = $1
			ORDER BY created_at, id`, keyID)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()

		var out []record
		for rows.Next() {
			var r record
			if err := rows.Scan(&r.clientID, &r.network, &r.rawTx, &r.txHash, &r.errorCode); err != nil {
				t.Fatal(err)
			}

			out = append(out, r)
		}
		return out
	}

	got := records(gas.ID)
	if len(got) != 2 {
		t.Fatalf("records = %d, want 2", len(got))
	}

	if r := got[0]; r.clientID != clientID || r.network != constants.NetworkEthereum || !bytes.Equal(r.rawTx, allowed) || r.txHash == nil || *r.txHash != out.TxHash || r.errorCode != nil {
		t.Fatalf("signed record = %+v", r)
	}

	if r := got[1]; !bytes.Equal(r.rawTx, denied) || r.txHash != nil || r.errorCode == nil || *r.errorCode != "POLICY_VIOLATION" {
		t.Fatalf("denied record = %+v", r)
	}

	if got := records(unknownKey); len(got) != 1 || got[0].errorCode == nil || *got[0].errorCode != "KEY_NOT_FOUND" {
		t.Fatalf("unknown key records = %+v", got)
	}
}
