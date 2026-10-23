package tron

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"math/big"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	tronlib "github.com/w3-developers/go-tron-lib"
	"google.golang.org/protobuf/encoding/protowire"

	"gitlab.tve-development.com/backend/gtoken/wallet-service/internal/policy"
)

func addressBytes(t *testing.T, address string) []byte {
	t.Helper()
	addressHex, err := tronlib.TronBase58ToHex(address)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := hex.DecodeString(addressHex)
	return b
}

func newAddress(t *testing.T) string {
	t.Helper()
	_, address, err := tronlib.GenerateAddress()
	if err != nil {
		t.Fatal(err)
	}
	return address
}

// contractMsg builds protocol.Transaction.Contract with parameter packed into google.protobuf.Any.
func contractMsg(contractType uint64, value []byte) []byte {
	var any []byte
	any = protowire.AppendTag(any, 1, protowire.BytesType) // type_url
	any = protowire.AppendBytes(any, []byte("type.googleapis.com/protocol.Contract"))
	any = protowire.AppendTag(any, anyValueField, protowire.BytesType)
	any = protowire.AppendBytes(any, value)

	var contract []byte
	contract = protowire.AppendTag(contract, contractTypeField, protowire.VarintType)
	contract = protowire.AppendVarint(contract, contractType)
	contract = protowire.AppendTag(contract, contractParameterField, protowire.BytesType)
	contract = protowire.AppendBytes(contract, any)
	return contract
}

func transferContract(owner, to []byte, amount uint64) []byte {
	var msg []byte
	msg = protowire.AppendTag(msg, ownerAddressField, protowire.BytesType)
	msg = protowire.AppendBytes(msg, owner)
	msg = protowire.AppendTag(msg, toAddressField, protowire.BytesType)
	msg = protowire.AppendBytes(msg, to)
	msg = protowire.AppendTag(msg, amountField, protowire.VarintType)
	msg = protowire.AppendVarint(msg, amount)
	return contractMsg(transferContractType, msg)
}

func triggerContract(owner, contract []byte, callValue uint64, data []byte, callTokenValue uint64) []byte {
	var msg []byte
	msg = protowire.AppendTag(msg, ownerAddressField, protowire.BytesType)
	msg = protowire.AppendBytes(msg, owner)
	msg = protowire.AppendTag(msg, toAddressField, protowire.BytesType)
	msg = protowire.AppendBytes(msg, contract)
	if callValue > 0 {
		msg = protowire.AppendTag(msg, amountField, protowire.VarintType)
		msg = protowire.AppendVarint(msg, callValue)
	}
	msg = protowire.AppendTag(msg, dataField, protowire.BytesType)
	msg = protowire.AppendBytes(msg, data)
	if callTokenValue > 0 {
		msg = protowire.AppendTag(msg, callTokenValueField, protowire.VarintType)
		msg = protowire.AppendVarint(msg, callTokenValue)
	}
	return contractMsg(triggerSmartContractType, msg)
}

func trc20Transfer(to []byte, amount int64) []byte {
	data := append([]byte{}, trc20TransferSelector...)
	data = append(data, make([]byte, 12)...)
	data = append(data, to[1:]...) // abi address is 20 bytes without 0x41
	return append(data, common.LeftPadBytes(big.NewInt(amount).Bytes(), 32)...)
}

// buildRawTx builds protocol.Transaction.raw with given expiration and contracts.
func buildRawTx(expiration time.Time, contracts ...[]byte) []byte {
	var raw []byte
	raw = protowire.AppendTag(raw, 1, protowire.BytesType) // ref_block_bytes
	raw = protowire.AppendBytes(raw, []byte{0x12, 0x34})
	raw = protowire.AppendTag(raw, 4, protowire.BytesType) // ref_block_hash
	raw = protowire.AppendBytes(raw, []byte{1, 2, 3, 4, 5, 6, 7, 8})
	raw = protowire.AppendTag(raw, rawExpirationField, protowire.VarintType)
	raw = protowire.AppendVarint(raw, uint64(expiration.UnixMilli()))
	for _, contract := range contracts {
		raw = protowire.AppendTag(raw, rawContractField, protowire.BytesType)
		raw = protowire.AppendBytes(raw, contract)
	}
	raw = protowire.AppendTag(raw, 14, protowire.VarintType) // timestamp
	raw = protowire.AppendVarint(raw, uint64(time.Now().UnixMilli()))

	return raw
}

func TestDecodeTx(t *testing.T) {
	signer := NewSigner()
	later := time.Now().Add(time.Minute)
	owner, to, usdt := newAddress(t), newAddress(t), newAddress(t)
	ownerB, toB, usdtB := addressBytes(t, owner), addressBytes(t, to), addressBytes(t, usdt)

	op, err := signer.DecodeTx(buildRawTx(later, transferContract(ownerB, toB, 1_600_000)))
	if err != nil || op.Op != policy.OpNative || op.From != owner || op.To != to || op.Amount.Int64() != 1_600_000 {
		t.Fatalf("native: op = %+v, err = %v", op, err)
	}

	op, err = signer.DecodeTx(buildRawTx(later, triggerContract(ownerB, usdtB, 0, trc20Transfer(toB, 500), 0)))
	if err != nil || op.Op != policy.OpToken || op.From != owner || op.Contract != usdt || op.To != to || op.Amount.Int64() != 500 {
		t.Fatalf("token: op = %+v, err = %v", op, err)
	}

	op, err = signer.DecodeTx(buildRawTx(later, triggerContract(ownerB, usdtB, 9, []byte{0xd5, 0xf1, 0x91, 0xb6, 0x01}, 0)))
	if err != nil || op.Op != policy.OpContractCall || op.Contract != usdt || op.Selector != [4]byte{0xd5, 0xf1, 0x91, 0xb6} || op.Value.Int64() != 9 {
		t.Fatalf("contract call: op = %+v, err = %v", op, err)
	}

	op, err = signer.DecodeTx(buildRawTx(later, triggerContract(ownerB, usdtB, 1, trc20Transfer(toB, 500), 0)))
	if err != nil || op.Op != policy.OpContractCall {
		t.Fatalf("transfer with call_value is contract call: op = %+v, err = %v", op, err)
	}

	cases := map[string][]byte{
		"expired":           buildRawTx(time.Now().Add(-time.Minute), transferContract(ownerB, toB, 1)),
		"no contract":       buildRawTx(later),
		"two contracts":     buildRawTx(later, transferContract(ownerB, toB, 1), transferContract(ownerB, toB, 1)),
		"permission update": buildRawTx(later, contractMsg(46, nil)),
		"trc10 call value":  buildRawTx(later, triggerContract(ownerB, usdtB, 0, trc20Transfer(toB, 1), 5)),
		"short call data":   buildRawTx(later, triggerContract(ownerB, usdtB, 0, []byte{0x01}, 0)),
		"bad owner address": buildRawTx(later, transferContract(ownerB[1:], toB, 1)),
		"garbage":           {0xff, 0xff, 0xff},
		"empty":             {},
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := signer.DecodeTx(raw); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestSignTx(t *testing.T) {
	privateKey, address, err := tronlib.GenerateAddress()
	if err != nil {
		t.Fatal(err)
	}
	raw := buildRawTx(time.Now().Add(time.Minute), transferContract(addressBytes(t, address), addressBytes(t, address), 1))

	signed, txID, err := NewSigner().SignTx(raw, []byte(privateKey))
	if err != nil {
		t.Fatalf("SignTx() error = %v", err)
	}

	hash := sha256.Sum256(raw)
	if txID != hex.EncodeToString(hash[:]) {
		t.Fatalf("txID = %s", txID)
	}

	num, typ, n := protowire.ConsumeTag(signed)
	if num != txRawDataField || typ != protowire.BytesType {
		t.Fatalf("first field = %d/%d", num, typ)
	}
	gotRaw, m := protowire.ConsumeBytes(signed[n:])
	if !bytes.Equal(gotRaw, raw) {
		t.Fatal("raw_data mismatch")
	}
	rest := signed[n+m:]

	num, typ, n = protowire.ConsumeTag(rest)
	if num != txSignatureField || typ != protowire.BytesType {
		t.Fatalf("second field = %d/%d", num, typ)
	}
	signature, m := protowire.ConsumeBytes(rest[n:])
	if len(signature) != 65 || len(rest[n+m:]) != 0 {
		t.Fatalf("signature len = %d, tail = %d", len(signature), len(rest[n+m:]))
	}

	pub, err := crypto.SigToPub(hash[:], signature)
	if err != nil {
		t.Fatal(err)
	}
	addressHex, err := tronlib.TronBase58ToHex(address)
	if err != nil {
		t.Fatal(err)
	}
	if want := "41" + hex.EncodeToString(crypto.PubkeyToAddress(*pub).Bytes()); addressHex != want {
		t.Fatalf("signer = %s, want %s", want, addressHex)
	}
}

func TestSignTxBadKey(t *testing.T) {
	if _, _, err := NewSigner().SignTx([]byte{0x01}, []byte("zz")); err == nil {
		t.Fatal("expected error")
	}
}
