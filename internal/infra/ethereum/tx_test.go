package ethereum

import (
	"encoding/hex"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"

	"gitlab.tve-development.com/backend/gtoken/wallet-service/internal/policy"
)

var (
	testChainID = big.NewInt(1)
	testTo      = common.HexToAddress("0x1111111111111111111111111111111111111111")
)

func newTestKey(t *testing.T) ([]byte, common.Address) {
	t.Helper()
	key, err := crypto.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}

	return []byte(hex.EncodeToString(crypto.FromECDSA(key))), crypto.PubkeyToAddress(key.PublicKey)
}

func marshalTx(t *testing.T, tx *types.Transaction) []byte {
	t.Helper()
	raw, err := tx.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}

	return raw
}

func TestSignTx(t *testing.T) {
	txs := map[string]*types.Transaction{
		"legacy": types.NewTransaction(7, testTo, big.NewInt(100), 21000, big.NewInt(5), nil),
		"dynamic_fee": types.NewTx(&types.DynamicFeeTx{
			ChainID:   testChainID,
			Nonce:     7,
			To:        &testTo,
			Value:     big.NewInt(100),
			Gas:       21000,
			GasFeeCap: big.NewInt(10),
			GasTipCap: big.NewInt(2),
		}),
	}

	for name, tx := range txs {
		t.Run(name, func(t *testing.T) {
			privateKey, address := newTestKey(t)

			signed, hash, err := SignTx(marshalTx(t, tx), privateKey, testChainID)
			if err != nil {
				t.Fatalf("SignTx() error = %v", err)
			}

			signedTx := new(types.Transaction)
			if err := signedTx.UnmarshalBinary(signed); err != nil {
				t.Fatal(err)
			}
			if signedTx.Hash().Hex() != hash || signedTx.ChainId().Cmp(testChainID) != 0 {
				t.Fatalf("hash = %s, chain id = %s", hash, signedTx.ChainId())
			}

			sender, err := types.Sender(types.LatestSignerForChainID(testChainID), signedTx)
			if err != nil || sender != address {
				t.Fatalf("sender = %s, err = %v, want %s", sender, err, address)
			}
		})
	}
}

func TestSignTxErrors(t *testing.T) {
	privateKey, _ := newTestKey(t)
	raw := marshalTx(t, types.NewTx(&types.DynamicFeeTx{ChainID: big.NewInt(56), To: &testTo, Gas: 21000}))

	if _, _, err := SignTx(raw, privateKey, testChainID); err == nil {
		t.Fatal("chain id mismatch: expected error")
	}
	if _, _, err := SignTx(raw, privateKey, nil); err == nil {
		t.Fatal("nil chain id: expected error")
	}
	if _, _, err := SignTx([]byte{0x01, 0x02}, privateKey, testChainID); err == nil {
		t.Fatal("bad tx: expected error")
	}
	if _, _, err := SignTx(raw, []byte("zz"), big.NewInt(56)); err == nil {
		t.Fatal("bad key: expected error")
	}
}

func TestDecodeTx(t *testing.T) {
	data := append(append([]byte{}, erc20TransferSelector...), make([]byte, 64)...)
	raw := marshalTx(t, types.NewTx(&types.DynamicFeeTx{
		ChainID:   testChainID,
		Nonce:     3,
		To:        &testTo,
		Value:     big.NewInt(0),
		Data:      data,
		Gas:       60000,
		GasFeeCap: big.NewInt(10),
		GasTipCap: big.NewInt(2),
	}))

	info, err := DecodeTx(raw)
	if err != nil {
		t.Fatalf("DecodeTx() error = %v", err)
	}
	if info.Type != types.DynamicFeeTxType || info.ChainID.Cmp(testChainID) != 0 || info.Nonce != 3 ||
		info.To != testTo.Hex() || info.Gas != 60000 || info.GasFeeCap.Int64() != 10 || info.GasTipCap.Int64() != 2 {
		t.Fatalf("info = %+v", info)
	}

	legacy, err := DecodeTx(marshalTx(t, types.NewTransaction(1, testTo, big.NewInt(5), 21000, big.NewInt(3), nil)))
	if err != nil || legacy.ChainID != nil || legacy.GasFeeCap.Int64() != 3 || legacy.Value.Int64() != 5 {
		t.Fatalf("legacy = %+v, err = %v", legacy, err)
	}
}

func TestDecodeERC20Transfer(t *testing.T) {
	recipient := common.HexToAddress("0x2222222222222222222222222222222222222222")
	data := append(append([]byte{}, erc20TransferSelector...), common.LeftPadBytes(recipient.Bytes(), 32)...)
	data = append(data, common.LeftPadBytes(big.NewInt(1500).Bytes(), 32)...)

	to, amount, ok := DecodeERC20Transfer(data)
	if !ok || to != recipient.Hex() || amount.Int64() != 1500 {
		t.Fatalf("to = %s, amount = %s, ok = %v", to, amount, ok)
	}

	if _, _, ok := DecodeERC20Transfer(data[:40]); ok {
		t.Fatal("short data: expected not ok")
	}
	if _, _, ok := DecodeERC20Transfer(append([]byte{0, 0, 0, 0}, data[4:]...)); ok {
		t.Fatal("other selector: expected not ok")
	}
}

func TestSignerDecodeTx(t *testing.T) {
	signer := NewSigner(testChainID)
	recipient := common.HexToAddress("0x2222222222222222222222222222222222222222")
	transfer := append(append([]byte{}, erc20TransferSelector...), common.LeftPadBytes(recipient.Bytes(), 32)...)
	transfer = append(transfer, common.LeftPadBytes(big.NewInt(1500).Bytes(), 32)...)
	call := []byte{0x7a, 0x45, 0xd9, 0x5c, 0x01}

	dynamic := func(value int64, data []byte) []byte {
		return marshalTx(t, types.NewTx(&types.DynamicFeeTx{ChainID: testChainID, To: &testTo, Value: big.NewInt(value), Gas: 60000, Data: data}))
	}

	op, err := signer.DecodeTx(marshalTx(t, types.NewTransaction(1, testTo, big.NewInt(7), 21000, big.NewInt(1), nil)))
	if err != nil || op.Op != policy.OpNative || op.To != testTo.Hex() || op.Amount.Int64() != 7 {
		t.Fatalf("legacy native: op = %+v, err = %v", op, err)
	}

	op, err = signer.DecodeTx(dynamic(0, transfer))
	if err != nil || op.Op != policy.OpToken || op.Contract != testTo.Hex() || op.To != recipient.Hex() || op.Amount.Int64() != 1500 {
		t.Fatalf("token: op = %+v, err = %v", op, err)
	}

	op, err = signer.DecodeTx(dynamic(3, transfer))
	if err != nil || op.Op != policy.OpContractCall || op.Value.Int64() != 3 {
		t.Fatalf("transfer with value is contract call: op = %+v, err = %v", op, err)
	}

	op, err = signer.DecodeTx(dynamic(0, call))
	if err != nil || op.Op != policy.OpContractCall || op.Contract != testTo.Hex() || op.Selector != [4]byte{0x7a, 0x45, 0xd9, 0x5c} {
		t.Fatalf("contract call: op = %+v, err = %v", op, err)
	}

	cases := map[string][]byte{
		"other chain":       marshalTx(t, types.NewTx(&types.DynamicFeeTx{ChainID: big.NewInt(56), To: &testTo, Gas: 21000})),
		"contract creation": marshalTx(t, types.NewTx(&types.DynamicFeeTx{ChainID: testChainID, Gas: 60000, Data: call})),
		"short call data":   dynamic(0, []byte{0x01, 0x02}),
		"bad tx":            {0x01, 0x02},
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := signer.DecodeTx(raw); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}
