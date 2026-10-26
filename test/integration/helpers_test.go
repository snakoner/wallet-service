//go:build integration

package integration

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"io"
	"math/big"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	aes256 "github.com/w3-developers/go-aes256"

	"gitlab.tve-development.com/backend/gtoken/wallet-service/internal/constants"
	keystorage "gitlab.tve-development.com/backend/gtoken/wallet-service/internal/features/key_storage"
	serviceclient "gitlab.tve-development.com/backend/gtoken/wallet-service/internal/features/service_client"
	"gitlab.tve-development.com/backend/gtoken/wallet-service/internal/infra/db"
	"gitlab.tve-development.com/backend/gtoken/wallet-service/internal/infra/logger"
)

// anvil dev account 0
const anvilFunderKey = "ac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80"

var anvilChainID = big.NewInt(31337)

type env struct {
	apiURL  string
	keys    *keystorage.Service
	clients *serviceclient.Service
	pool    *pgxpool.Pool
	eth     *ethclient.Client
	http    *http.Client
	service string
	apiKey  string
	ctx     context.Context
	tb      testing.TB
}

func newEnv(t *testing.T) *env {
	t.Helper()
	dsn, apiURL, rpc, kekHex := os.Getenv("WALLET_TEST_DATABASE_URL"), os.Getenv("WALLET_TEST_API_URL"), os.Getenv("WALLET_TEST_ETH_RPC"), os.Getenv("WALLET_TEST_KEK")
	if dsn == "" || apiURL == "" || rpc == "" || kekHex == "" {
		t.Skip("WALLET_TEST_* env is not set, run make test-integration")
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	t.Cleanup(cancel)

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)

	eth, err := ethclient.DialContext(ctx, rpc)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(eth.Close)

	kek, err := hex.DecodeString(kekHex)
	if err != nil {
		t.Fatal(err)
	}

	log := logger.New(logger.Config{Level: "error"})
	txm := db.NewPgxTxManager(pool)
	e := &env{
		apiURL:  apiURL,
		keys:    keystorage.NewService(log, keystorage.NewRepository(), txm, aes256.NewAES256GCM(kek), 1),
		clients: serviceclient.NewService(log, serviceclient.NewRepository(), txm),
		pool:    pool,
		eth:     eth,
		http:    &http.Client{Timeout: 15 * time.Second},
		service: "it-" + uuid.NewString()[:8],
		ctx:     ctx,
		tb:      t,
	}

	e.apiKey, _, err = e.clients.CreateClient(ctx, e.service)
	if err != nil {
		t.Fatal(err)
	}

	return e
}

func (e *env) role(purpose string, network constants.Network, kind keystorage.RoleKind, policy string) *keystorage.Role {
	e.tb.Helper()
	role, err := e.keys.CreateRole(e.ctx, keystorage.CreateRoleParams{Service: e.service, Purpose: purpose, Network: network, Kind: kind, Policy: []byte(policy)})
	if err != nil {
		e.tb.Fatal(err)
	}
	return role
}

func (e *env) key(role *keystorage.Role, externalRef string) *keystorage.WalletKey {
	e.tb.Helper()
	params := keystorage.CreateKeyParams{RoleID: role.ID}
	if externalRef != "" {
		params.ExternalRef = &externalRef
	}

	key, err := e.keys.CreateKey(e.ctx, params)
	if err != nil {
		e.tb.Fatal(err)
	}
	return key
}

type apiResponse struct {
	Success bool            `json:"success"`
	Data    json.RawMessage `json:"data"`
	Error   struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func (e *env) call(method, path, apiKey string, body any, out any) (int, apiResponse) {
	e.tb.Helper()
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			e.tb.Fatal(err)
		}
		reader = bytes.NewReader(data)
	}

	req, err := http.NewRequestWithContext(e.ctx, method, e.apiURL+path, reader)
	if err != nil {
		e.tb.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	if apiKey != "" {
		req.Header.Set("X-Service-Key", apiKey)
	}

	resp, err := e.http.Do(req)
	if err != nil {
		e.tb.Fatal(err)
	}
	defer resp.Body.Close()

	var res apiResponse
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		e.tb.Fatalf("%s %s: status %d, decode: %v", method, path, resp.StatusCode, err)
	}
	if out != nil && res.Success {
		if err := json.Unmarshal(res.Data, out); err != nil {
			e.tb.Fatal(err)
		}
	}

	return resp.StatusCode, res
}

type signed struct {
	SignedTx string `json:"signed_tx"`
	TxHash   string `json:"tx_hash"`
}

func (e *env) sign(keyID uuid.UUID, network string, rawTx []byte) (int, apiResponse, signed) {
	e.tb.Helper()
	var out signed
	status, res := e.call(http.MethodPost, "/api/v1/keys/"+keyID.String()+"/sign-transaction", e.apiKey,
		map[string]string{"network": network, "raw_tx": "0x" + hex.EncodeToString(rawTx)}, &out)
	return status, res, out
}

func (e *env) fund(address string, wei *big.Int) {
	e.tb.Helper()
	funder, err := crypto.HexToECDSA(anvilFunderKey)
	if err != nil {
		e.tb.Fatal(err)
	}

	from := crypto.PubkeyToAddress(funder.PublicKey)
	nonce, err := e.eth.PendingNonceAt(e.ctx, from)
	if err != nil {
		e.tb.Fatal(err)
	}

	to := common.HexToAddress(address)
	tx, err := types.SignTx(e.unsignedTx(nonce, to, wei, true), types.LatestSignerForChainID(anvilChainID), funder)
	if err != nil {
		e.tb.Fatal(err)
	}

	e.broadcast(tx)
}

func (e *env) unsignedTx(nonce uint64, to common.Address, value *big.Int, dynamic bool) *types.Transaction {
	e.tb.Helper()
	gas, err := e.eth.EstimateGas(e.ctx, ethereum.CallMsg{To: &to, Value: value})
	if err != nil {
		e.tb.Fatal(err)
	}

	gasPrice, err := e.eth.SuggestGasPrice(e.ctx)
	if err != nil {
		e.tb.Fatal(err)
	}

	if !dynamic {
		return types.NewTransaction(nonce, to, value, gas, gasPrice, nil)
	}

	return types.NewTx(&types.DynamicFeeTx{
		ChainID:   anvilChainID,
		Nonce:     nonce,
		To:        &to,
		Value:     value,
		Gas:       gas,
		GasFeeCap: new(big.Int).Mul(gasPrice, big.NewInt(2)),
		GasTipCap: big.NewInt(1),
	})
}

func (e *env) broadcast(tx *types.Transaction) *types.Receipt {
	e.tb.Helper()
	if err := e.eth.SendTransaction(e.ctx, tx); err != nil {
		e.tb.Fatalf("send tx: %v", err)
	}

	for {
		receipt, err := e.eth.TransactionReceipt(e.ctx, tx.Hash())
		if err == nil {
			if receipt.Status != types.ReceiptStatusSuccessful {
				e.tb.Fatalf("tx %s reverted", tx.Hash())
			}
			return receipt
		}

		select {
		case <-e.ctx.Done():
			e.tb.Fatalf("wait receipt: %v", e.ctx.Err())
		case <-time.After(100 * time.Millisecond):
		}
	}
}

func (e *env) setStatus(keyID uuid.UUID, status keystorage.KeyStatus) {
	e.tb.Helper()
	if _, err := e.pool.Exec(e.ctx, `UPDATE wallet_keys SET status = $2 WHERE id = $1`, keyID, status); err != nil {
		e.tb.Fatal(err)
	}
}
