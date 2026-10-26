package signing

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/google/uuid"

	"gitlab.tve-development.com/backend/gtoken/wallet-service/internal/constants"
	keystorage "gitlab.tve-development.com/backend/gtoken/wallet-service/internal/features/key_storage"
	"gitlab.tve-development.com/backend/gtoken/wallet-service/internal/infra/ethereum"
	"gitlab.tve-development.com/backend/gtoken/wallet-service/internal/infra/logger"
	"gitlab.tve-development.com/backend/gtoken/wallet-service/internal/policy"
)

var testTo = common.HexToAddress("0x1111111111111111111111111111111111111111")

type keyProviderStub struct {
	key        *keystorage.WalletKey
	privateKey []byte
	err        error
	service    string
	keyID      uuid.UUID
	network    constants.Network
	called     bool
	signed     bool
	leaseID    *uuid.UUID
	internal   map[string]string // address -> purpose
}

func (p *keyProviderStub) UseSigningKey(
	_ context.Context,
	service string,
	keyID uuid.UUID,
	network constants.Network,
	leaseID *uuid.UUID,
	check func(key *keystorage.WalletKey) error,
	fn func(key *keystorage.WalletKey, privateKey []byte) error,
) error {
	p.called, p.service, p.keyID, p.network, p.leaseID = true, service, keyID, network, leaseID
	if p.err != nil {
		return p.err
	}

	if err := check(p.key); err != nil {
		return err
	}

	p.signed = true
	return fn(p.key, p.privateKey)
}

func (p *keyProviderStub) IsServiceAddress(_ context.Context, service string, _ constants.Network, address, purpose string) (bool, error) {
	got, ok := p.internal[address]
	return ok && service == p.key.Role.Service && (purpose == "" || purpose == got), nil
}

func newKeyProvider(t *testing.T) *keyProviderStub {
	t.Helper()
	privateKey, err := crypto.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}

	return &keyProviderStub{
		key: &keystorage.WalletKey{
			ID:      uuid.New(),
			Role:    keystorage.Role{Service: "exchange", Purpose: "executor", Policy: json.RawMessage(`{"version":1,"rules":[{"op":"native","to":["*"]}]}`)},
			Network: constants.NetworkEthereum,
			Address: crypto.PubkeyToAddress(privateKey.PublicKey).Hex(),
		},
		privateKey: []byte(hex.EncodeToString(crypto.FromECDSA(privateKey))),
	}
}

func newTestService(keys keyProvider) *Service {
	return NewService(logger.New(logger.Config{Level: "error"}), keys, map[constants.Network]TxSigner{
		constants.NetworkEthereum: ethereum.NewSigner(big.NewInt(1)),
	})
}

func marshalTx(t *testing.T, tx *types.Transaction) []byte {
	t.Helper()
	raw, err := tx.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}

	return raw
}

func TestSignTransaction(t *testing.T) {
	txs := map[string]*types.Transaction{
		"legacy":      types.NewTransaction(1, testTo, big.NewInt(10), 21000, big.NewInt(5), nil),
		"dynamic_fee": types.NewTx(&types.DynamicFeeTx{ChainID: big.NewInt(1), Nonce: 1, To: &testTo, Value: big.NewInt(10), Gas: 21000, GasFeeCap: big.NewInt(5), GasTipCap: big.NewInt(1)}),
	}

	for name, tx := range txs {
		t.Run(name, func(t *testing.T) {
			keys := newKeyProvider(t)
			srv := newTestService(keys)

			signed, err := srv.SignTransaction(context.Background(), SignTransactionParams{
				Service: "exchange",
				KeyID:   keys.key.ID,
				Network: constants.NetworkEthereum,
				RawTx:   marshalTx(t, tx),
			})
			if err != nil {
				t.Fatalf("SignTransaction() error = %v", err)
			}

			if keys.service != "exchange" || keys.keyID != keys.key.ID || keys.network != constants.NetworkEthereum {
				t.Fatalf("provider got service = %q, key = %s, network = %s", keys.service, keys.keyID, keys.network)
			}

			signedTx := new(types.Transaction)
			if err := signedTx.UnmarshalBinary(signed.SignedTx); err != nil {
				t.Fatal(err)
			}
			sender, err := types.Sender(types.LatestSignerForChainID(big.NewInt(1)), signedTx)
			if err != nil || sender.Hex() != keys.key.Address || signedTx.Hash().Hex() != signed.TxHash {
				t.Fatalf("sender = %s, err = %v, want %s", sender.Hex(), err, keys.key.Address)
			}
		})
	}
}

func TestSignTransactionErrors(t *testing.T) {
	raw := marshalTx(t, types.NewTransaction(1, testTo, big.NewInt(10), 21000, big.NewInt(5), nil))
	otherChain := marshalTx(t, types.NewTx(&types.DynamicFeeTx{ChainID: big.NewInt(56), To: &testTo, Gas: 21000}))

	cases := []struct {
		name       string
		network    constants.Network
		raw        []byte
		want       error
		wantCalled bool
	}{
		{"no signer for network", constants.NetworkTron, raw, keystorage.ErrUnsupportedNetwork, false},
		{"bad tx", constants.NetworkEthereum, []byte{0x01, 0x02}, ErrInvalidTransaction, false},
		{"other chain", constants.NetworkEthereum, otherChain, ErrInvalidTransaction, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			keys := newKeyProvider(t)
			_, err := newTestService(keys).SignTransaction(context.Background(), SignTransactionParams{Service: "exchange", KeyID: keys.key.ID, Network: tc.network, RawTx: tc.raw})
			if !errors.Is(err, tc.want) || keys.called != tc.wantCalled {
				t.Fatalf("err = %v, want %v, provider called = %v", err, tc.want, keys.called)
			}
		})
	}

	keys := newKeyProvider(t)
	keys.err = keystorage.ErrKeyNotActive
	if _, err := newTestService(keys).SignTransaction(context.Background(), SignTransactionParams{Service: "exchange", KeyID: keys.key.ID, Network: constants.NetworkEthereum, RawTx: raw}); !errors.Is(err, keystorage.ErrKeyNotActive) {
		t.Fatalf("provider error: err = %v", err)
	}
}

type stubSigner struct{ op policy.Operation }

func (s stubSigner) DecodeTx([]byte) (*policy.Operation, error) { return &s.op, nil }

func (s stubSigner) SignTx([]byte, []byte) ([]byte, string, error) { return []byte{1}, "txid", nil }

func TestSignTransactionPolicy(t *testing.T) {
	native := marshalTx(t, types.NewTransaction(1, testTo, big.NewInt(10), 21000, big.NewInt(5), nil))
	call := marshalTx(t, types.NewTx(&types.DynamicFeeTx{ChainID: big.NewInt(1), To: &testTo, Gas: 60000, Data: []byte{0xa9, 0x05, 0x9c, 0xbb, 0x01}}))

	cases := []struct {
		name     string
		policy   string
		internal map[string]string
		raw      []byte
		want     error
	}{
		{"empty policy denies", `{}`, nil, native, ErrPolicyViolation},
		{"native to internal gas station", `{"version":1,"rules":[{"op":"native","to":["internal:gas_station"]}]}`, map[string]string{testTo.Hex(): "gas_station"}, native, nil},
		{"native to internal of other purpose", `{"version":1,"rules":[{"op":"native","to":["internal:gas_station"]}]}`, map[string]string{testTo.Hex(): "executor"}, native, ErrPolicyViolation},
		{"native to stranger", `{"version":1,"rules":[{"op":"native","to":["internal"]}]}`, nil, native, ErrPolicyViolation},
		{"contract call not allowed", `{"version":1,"rules":[{"op":"native","to":["*"]}]}`, nil, call, ErrPolicyViolation},
		{"over max amount", `{"version":1,"rules":[{"op":"native","to":["*"],"max_amount":"9"}]}`, nil, native, ErrPolicyViolation},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			keys := newKeyProvider(t)
			keys.key.Role.Policy = json.RawMessage(tc.policy)
			keys.internal = tc.internal

			_, err := newTestService(keys).SignTransaction(context.Background(), SignTransactionParams{Service: "exchange", KeyID: keys.key.ID, Network: constants.NetworkEthereum, RawTx: tc.raw})
			if !errors.Is(err, tc.want) || keys.signed != (tc.want == nil) {
				t.Fatalf("err = %v, want %v, signed = %v", err, tc.want, keys.signed)
			}
		})
	}
}

func TestSignTransactionOwnerMismatch(t *testing.T) {
	keys := newKeyProvider(t)
	srv := NewService(logger.New(logger.Config{Level: "error"}), keys, map[constants.Network]TxSigner{
		constants.NetworkEthereum: stubSigner{op: policy.Operation{Op: policy.OpNative, From: "TOtherOwner", To: testTo.Hex(), Amount: big.NewInt(1)}},
	})

	_, err := srv.SignTransaction(context.Background(), SignTransactionParams{Service: "exchange", KeyID: keys.key.ID, Network: constants.NetworkEthereum, RawTx: []byte{1}})
	if !errors.Is(err, ErrInvalidTransaction) || keys.signed {
		t.Fatalf("err = %v, signed = %v", err, keys.signed)
	}
}

func TestSignTransactionToReplacement(t *testing.T) {
	replacement := common.HexToAddress("0x5555555555555555555555555555555555555555")
	native := func(to common.Address) []byte {
		return marshalTx(t, types.NewTransaction(1, to, big.NewInt(10), 21000, big.NewInt(5), nil))
	}
	call := marshalTx(t, types.NewTx(&types.DynamicFeeTx{ChainID: big.NewInt(1), To: &replacement, Gas: 60000, Data: []byte{0x12, 0x34, 0x56, 0x78}}))

	cases := []struct {
		name string
		raw  []byte
		want error
	}{
		{"native to replacement", native(replacement), nil},
		{"native to other address", native(testTo), ErrPolicyViolation},
		{"contract call to replacement", call, ErrPolicyViolation},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			keys := newKeyProvider(t)
			keys.key.Role.Policy = json.RawMessage(`{}`)
			address := replacement.Hex()
			keys.key.ReplacementAddress = &address

			_, err := newTestService(keys).SignTransaction(context.Background(), SignTransactionParams{Service: "exchange", KeyID: keys.key.ID, Network: constants.NetworkEthereum, RawTx: tc.raw})
			if !errors.Is(err, tc.want) || keys.signed != (tc.want == nil) {
				t.Fatalf("err = %v, want %v, signed = %v", err, tc.want, keys.signed)
			}
		})
	}
}

func TestSignTransactionPassesLease(t *testing.T) {
	keys := newKeyProvider(t)
	leaseID := uuid.New()
	raw := marshalTx(t, types.NewTransaction(1, testTo, big.NewInt(10), 21000, big.NewInt(5), nil))

	if _, err := newTestService(keys).SignTransaction(context.Background(), SignTransactionParams{Service: "exchange", KeyID: keys.key.ID, Network: constants.NetworkEthereum, RawTx: raw, LeaseID: &leaseID}); err != nil {
		t.Fatal(err)
	}
	if keys.leaseID == nil || *keys.leaseID != leaseID {
		t.Fatalf("lease passed to key provider = %v", keys.leaseID)
	}
}
