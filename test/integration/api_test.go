//go:build integration

package integration

import (
	"crypto/sha256"
	"encoding/hex"
	"math/big"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/google/uuid"
	tronlib "github.com/w3-developers/go-tron-lib"
	"google.golang.org/protobuf/encoding/protowire"

	"gitlab.tve-development.com/backend/gtoken/wallet-service/internal/constants"
)

const gasStationPolicy = `{"version":1,"rules":[{"op":"native","to":["internal:deposit"]}]}`

var oneEth = big.NewInt(1_000_000_000_000_000_000)

func TestAuth(t *testing.T) {
	e := newEnv(t)
	path := "/api/v1/roles/gas_station/key?network=ethereum"

	for name, key := range map[string]string{"no key": "", "wrong key": strings.Repeat("0", 64)} {
		if status, res := e.call(http.MethodGet, path, key, nil, nil); status != http.StatusUnauthorized {
			t.Fatalf("%s: status = %d, error = %+v", name, status, res.Error)
		}
	}

	clients, err := e.clients.ListClients(e.ctx, e.service)
	if err != nil {
		t.Fatal(err)
	}

	if err := e.clients.RevokeClient(e.ctx, clients[0].ID); err != nil {
		t.Fatal(err)
	}

	if status, _ := e.call(http.MethodGet, path, e.apiKey, nil, nil); status != http.StatusUnauthorized {
		t.Fatalf("revoked key: status = %d", status)
	}
}

func TestRoleKey(t *testing.T) {
	e := newEnv(t)
	gas := e.key(e.role("gas_station", constants.NetworkEthereum, constants.RoleKindSingle, gasStationPolicy), "")

	var key struct {
		ID      uuid.UUID `json:"id"`
		Address string    `json:"address"`
		Kind    string    `json:"kind"`
	}
	status, res := e.call(http.MethodGet, "/api/v1/roles/gas_station/key?network=ethereum", e.apiKey, nil, &key)
	if status != http.StatusOK || key.ID != gas.ID || key.Address != gas.Address || key.Kind != "single" {
		t.Fatalf("status = %d, key = %+v, error = %+v", status, key, res.Error)
	}

	other := newEnv(t)
	if status, res := other.call(http.MethodGet, "/api/v1/roles/gas_station/key?network=ethereum", other.apiKey, nil, nil); status != http.StatusNotFound || res.Error.Code != "ROLE_NOT_FOUND" {
		t.Fatalf("other service: status = %d, error = %+v", status, res.Error)
	}

	pool := e.role("executor", constants.NetworkEthereum, constants.RoleKindPool, "")
	e.key(pool, "")
	e.key(pool, "")
	var keys []map[string]any
	if status, _ := e.call(http.MethodGet, "/api/v1/roles/executor/keys?network=ethereum", e.apiKey, nil, &keys); status != http.StatusOK || len(keys) != 2 {
		t.Fatalf("pool keys: status = %d, keys = %v", status, keys)
	}
}

func TestSignEthereumBroadcast(t *testing.T) {
	e := newEnv(t)
	gas := e.key(e.role("gas_station", constants.NetworkEthereum, constants.RoleKindSingle, gasStationPolicy), "")
	deposit := e.key(e.role("deposit", constants.NetworkEthereum, constants.RoleKindDeposit, ""), "order-1")
	e.fund(gas.Address, oneEth)

	from, to := common.HexToAddress(gas.Address), common.HexToAddress(deposit.Address)
	value := big.NewInt(1_000_000_000_000_000)

	parent := e.tb
	for name, dynamic := range map[string]bool{"legacy": false, "dynamic_fee": true} {
		t.Run(name, func(t *testing.T) {
			e.tb = t
			defer func() { e.tb = parent }()
			nonce, err := e.eth.PendingNonceAt(e.ctx, from)
			if err != nil {
				t.Fatal(err)
			}

			raw, err := e.unsignedTx(nonce, to, value, dynamic).MarshalBinary()
			if err != nil {
				t.Fatal(err)
			}

			status, res, out := e.sign(gas.ID, "ethereum", raw)
			if status != http.StatusOK {
				t.Fatalf("sign: status = %d, error = %+v", status, res.Error)
			}

			tx := new(types.Transaction)
			if err := tx.UnmarshalBinary(common.FromHex(out.SignedTx)); err != nil {
				t.Fatal(err)
			}

			if tx.Hash().Hex() != out.TxHash {
				t.Fatalf("tx_hash = %s, want %s", out.TxHash, tx.Hash().Hex())
			}

			receipt := e.broadcast(tx)
			sender, err := types.Sender(types.LatestSignerForChainID(anvilChainID), tx)
			if err != nil || sender != from || receipt.Status != types.ReceiptStatusSuccessful {
				t.Fatalf("sender = %s, err = %v, want %s", sender, err, from)
			}
		})
	}

	balance, err := e.eth.BalanceAt(e.ctx, to, nil)
	if err != nil || balance.Cmp(new(big.Int).Mul(value, big.NewInt(2))) != 0 {
		t.Fatalf("deposit balance = %s, err = %v", balance, err)
	}
}

func TestSignRejected(t *testing.T) {
	e := newEnv(t)
	gasRole := e.role("gas_station", constants.NetworkEthereum, constants.RoleKindSingle, gasStationPolicy)
	gas := e.key(gasRole, "")
	deposit := e.key(e.role("deposit", constants.NetworkEthereum, constants.RoleKindDeposit, ""), "order-1")
	value := big.NewInt(1)

	toDeposit, err := e.unsignedTx(0, common.HexToAddress(deposit.Address), value, true).MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}

	toStranger, err := e.unsignedTx(0, common.HexToAddress("0x4444444444444444444444444444444444444444"), value, true).MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}

	otherChain, err := types.NewTx(&types.DynamicFeeTx{ChainID: big.NewInt(1), To: &common.Address{1}, Gas: 21000}).MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name   string
		keyID  uuid.UUID
		raw    []byte
		status int
		code   string
	}{
		{"policy: stranger recipient", gas.ID, toStranger, http.StatusForbidden, "POLICY_VIOLATION"},
		{"other chain", gas.ID, otherChain, http.StatusBadRequest, "INVALID_TRANSACTION"},
		{"garbage tx", gas.ID, []byte{1, 2, 3}, http.StatusBadRequest, "INVALID_TRANSACTION"},
		{"unknown key", uuid.New(), toDeposit, http.StatusNotFound, "KEY_NOT_FOUND"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if status, res, _ := e.sign(tc.keyID, "ethereum", tc.raw); status != tc.status || res.Error.Code != tc.code {
				t.Fatalf("status = %d, error = %+v", status, res.Error)
			}
		})
	}

	other := newEnv(t)
	if status, res, _ := other.sign(gas.ID, "ethereum", toDeposit); status != http.StatusNotFound || res.Error.Code != "KEY_NOT_FOUND" {
		t.Fatalf("other service: status = %d, error = %+v", status, res.Error)
	}

	e.setStatus(gas.ID, constants.KeyStatusDisabled)
	if status, res, _ := e.sign(gas.ID, "ethereum", toDeposit); status != http.StatusConflict || res.Error.Code != "KEY_NOT_ACTIVE" {
		t.Fatalf("disabled key: status = %d, error = %+v", status, res.Error)
	}
}

func TestSignTron(t *testing.T) {
	e := newEnv(t)
	gas := e.key(e.role("gas_station", constants.NetworkTron, constants.RoleKindSingle, `{"version":1,"rules":[{"op":"native","to":["*"]}]}`), "")
	_, recipient, err := tronlib.GenerateAddress()
	if err != nil {
		t.Fatal(err)
	}

	raw := tronTransferRaw(t, gas.Address, recipient, 1_600_000, time.Now().Add(time.Minute))
	status, res, out := e.sign(gas.ID, "tron", raw)
	if status != http.StatusOK {
		t.Fatalf("sign: status = %d, error = %+v", status, res.Error)
	}

	hash := sha256.Sum256(raw)
	if out.TxHash != hex.EncodeToString(hash[:]) {
		t.Fatalf("tx_hash = %s", out.TxHash)
	}

	signedTx := common.FromHex(out.SignedTx)
	_, _, n := protowire.ConsumeTag(signedTx)
	_, m := protowire.ConsumeBytes(signedTx[n:])
	_, _, k := protowire.ConsumeTag(signedTx[n+m:])
	signature, _ := protowire.ConsumeBytes(signedTx[n+m+k:])

	pub, err := crypto.SigToPub(hash[:], signature)
	if err != nil {
		t.Fatal(err)
	}

	signer, err := tronlib.TronHexToBase58("41" + hex.EncodeToString(crypto.PubkeyToAddress(*pub).Bytes()))
	if err != nil || signer != gas.Address {
		t.Fatalf("signer = %s, err = %v, want %s", signer, err, gas.Address)
	}

	_, stranger, _ := tronlib.GenerateAddress()
	if status, res, _ := e.sign(gas.ID, "tron", tronTransferRaw(t, stranger, recipient, 1, time.Now().Add(time.Minute))); status != http.StatusBadRequest || res.Error.Code != "INVALID_TRANSACTION" {
		t.Fatalf("other owner: status = %d, error = %+v", status, res.Error)
	}

	if status, res, _ := e.sign(gas.ID, "tron", tronTransferRaw(t, gas.Address, recipient, 1, time.Now().Add(-time.Minute))); status != http.StatusBadRequest || res.Error.Code != "INVALID_TRANSACTION" {
		t.Fatalf("expired tx: status = %d, error = %+v", status, res.Error)
	}
}

func tronTransferRaw(t *testing.T, owner, to string, amount uint64, expiration time.Time) []byte {
	t.Helper()
	address := func(base58 string) []byte {
		addressHex, err := tronlib.TronBase58ToHex(base58)
		if err != nil {
			t.Fatal(err)
		}
		b, _ := hex.DecodeString(addressHex)
		return b
	}

	var transfer []byte
	transfer = protowire.AppendTag(transfer, 1, protowire.BytesType) // owner_address
	transfer = protowire.AppendBytes(transfer, address(owner))
	transfer = protowire.AppendTag(transfer, 2, protowire.BytesType) // to_address
	transfer = protowire.AppendBytes(transfer, address(to))
	transfer = protowire.AppendTag(transfer, 3, protowire.VarintType) // amount
	transfer = protowire.AppendVarint(transfer, amount)

	var anyMsg []byte
	anyMsg = protowire.AppendTag(anyMsg, 1, protowire.BytesType) // type_url
	anyMsg = protowire.AppendBytes(anyMsg, []byte("type.googleapis.com/protocol.TransferContract"))
	anyMsg = protowire.AppendTag(anyMsg, 2, protowire.BytesType) // value
	anyMsg = protowire.AppendBytes(anyMsg, transfer)

	var contract []byte
	contract = protowire.AppendTag(contract, 1, protowire.VarintType) // type: TransferContract
	contract = protowire.AppendVarint(contract, 1)
	contract = protowire.AppendTag(contract, 2, protowire.BytesType) // parameter
	contract = protowire.AppendBytes(contract, anyMsg)

	var raw []byte
	raw = protowire.AppendTag(raw, 1, protowire.BytesType) // ref_block_bytes
	raw = protowire.AppendBytes(raw, []byte{0x12, 0x34})
	raw = protowire.AppendTag(raw, 4, protowire.BytesType) // ref_block_hash
	raw = protowire.AppendBytes(raw, []byte{1, 2, 3, 4, 5, 6, 7, 8})
	raw = protowire.AppendTag(raw, 8, protowire.VarintType) // expiration
	raw = protowire.AppendVarint(raw, uint64(expiration.UnixMilli()))
	raw = protowire.AppendTag(raw, 11, protowire.BytesType) // contract
	raw = protowire.AppendBytes(raw, contract)
	raw = protowire.AppendTag(raw, 14, protowire.VarintType) // timestamp
	raw = protowire.AppendVarint(raw, uint64(time.Now().UnixMilli()))

	return raw
}

func TestAdminAPI(t *testing.T) {
	e := newEnv(t)

	var role struct {
		ID     uuid.UUID `json:"id"`
		Policy any       `json:"policy"`
	}
	status, res := e.call(http.MethodPost, "/api/v1/admin/roles", "", map[string]any{
		"service": e.service, "purpose": "executor", "network": "ethereum", "kind": "pool",
		"policy": map[string]any{"version": 1, "rules": []any{map[string]any{"op": "native", "to": []string{"*"}}}},
	}, &role)
	if status != http.StatusOK {
		t.Fatalf("create role: status = %d, error = %+v", status, res.Error)
	}

	for range 2 {
		if status, res := e.call(http.MethodPost, "/api/v1/admin/roles/"+role.ID.String()+"/keys", "", nil, nil); status != http.StatusOK {
			t.Fatalf("create key: status = %d, error = %+v", status, res.Error)
		}
	}

	var page struct {
		Keys []struct {
			Service string `json:"service"`
			Address string `json:"address"`
		} `json:"keys"`
		Total int64 `json:"total"`
	}
	if status, _ := e.call(http.MethodGet, "/api/v1/admin/keys?service="+e.service+"&network=ethereum&status=active&limit=1", "", nil, &page); status != http.StatusOK || len(page.Keys) != 1 || page.Total != 2 || page.Keys[0].Service != e.service {
		t.Fatalf("list keys: status = %d, page = %+v", status, page)
	}

	if status, res := e.call(http.MethodPut, "/api/v1/admin/roles/"+role.ID.String()+"/policy", "", map[string]any{"policy": map[string]any{"version": 1, "rules": []any{map[string]any{"op": "token"}}}}, nil); status != http.StatusBadRequest || res.Error.Code != "INVALID_PARAMS" {
		t.Fatalf("invalid policy: status = %d, error = %+v", status, res.Error)
	}
}
