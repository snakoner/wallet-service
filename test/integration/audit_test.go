//go:build integration

package integration

import (
	"bytes"
	"encoding/hex"
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

	type auditView struct {
		Service   string    `json:"service"`
		KeyID     uuid.UUID `json:"key_id"`
		Network   string    `json:"network"`
		RawTx     string    `json:"raw_tx"`
		TxHash    *string   `json:"tx_hash"`
		ErrorCode *string   `json:"error_code"`
	}
	list := func(query string) []auditView {
		t.Helper()
		var views []auditView
		if status, res := e.call(http.MethodGet, "/api/v1/admin/audit?"+query, "", nil, &views); status != http.StatusOK {
			t.Fatalf("audit ?%s: status = %d, error = %+v", query, status, res.Error)
		}
		return views
	}

	byKey := "key_id=" + gas.ID.String()
	got2 := list(byKey)
	if len(got2) != 2 {
		t.Fatalf("api records = %d, want 2", len(got2))
	}
	if r := got2[0]; r.ErrorCode == nil || *r.ErrorCode != "POLICY_VIOLATION" || r.RawTx != "0x"+hex.EncodeToString(denied) {
		t.Fatalf("newest api record = %+v", r)
	}
	if r := got2[1]; r.Service != e.service || r.KeyID != gas.ID || r.Network != "ethereum" || r.RawTx != "0x"+hex.EncodeToString(allowed) || r.TxHash == nil || *r.TxHash != out.TxHash || r.ErrorCode != nil {
		t.Fatalf("signed api record = %+v", r)
	}

	for _, tc := range []struct {
		query     string
		want      int
		wantError string // error code of first record, "" - signed
	}{
		{byKey + "&limit=1", 1, "POLICY_VIOLATION"},
		{byKey + "&offset=1&limit=1", 1, ""},
		{byKey + "&offset=2", 0, ""},
		{byKey + "&failed=true", 1, "POLICY_VIOLATION"},
		{byKey + "&failed=false", 1, ""},
		{byKey + "&from=2000-01-01T00:00:00Z", 2, "POLICY_VIOLATION"},
		{byKey + "&to=2000-01-01T00:00:00Z", 0, ""},
		{"tx_hash=" + out.TxHash, 1, ""},
		{"service=" + e.service + "&error_code=KEY_NOT_FOUND", 1, "KEY_NOT_FOUND"},
	} {
		got := list(tc.query)
		if len(got) != tc.want {
			t.Fatalf("?%s: records = %d, want %d", tc.query, len(got), tc.want)
		}
		if len(got) > 0 && (got[0].ErrorCode == nil && tc.wantError != "" || got[0].ErrorCode != nil && *got[0].ErrorCode != tc.wantError) {
			t.Fatalf("?%s: first record = %+v, want error %q", tc.query, got[0], tc.wantError)
		}
	}

	for _, query := range []string{"key_id=1", "from=yesterday"} {
		if status, _ := e.call(http.MethodGet, "/api/v1/admin/audit?"+query, "", nil, nil); status != http.StatusBadRequest {
			t.Fatalf("?%s: status = %d, want 400", query, status)
		}
	}
}
