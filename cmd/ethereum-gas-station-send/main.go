package main

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"math/big"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
)

const usage = `ethereum-gas-station-send - send ETH from gas station via wallet service

Usage:
  ethereum-gas-station-send -to <address> -amount <eth> [flags]

Flags:
  -to <address>        recipient
  -amount <eth>        amount in ETH, e.g. 0.001
  -purpose <purpose>   gas station role purpose (default gas_station)
  -label <label>       gas station label, e.g. atoken
  -send                broadcast signed tx, otherwise only print it
  -api <url>           wallet service url (env WALLET_API_URL, default http://localhost:8080)
  -service-key <key>   X-Service-Key (env WALLET_SERVICE_KEY)
  -rpc <url>           ethereum rpc for nonce and fees (env ETH_RPC_URL)

Example:
  WALLET_SERVICE_KEY=... ETH_RPC_URL=https://... ethereum-gas-station-send -to 0x... -amount 0.001 -send
`

type apiResponse struct {
	Success bool            `json:"success"`
	Data    json.RawMessage `json:"data"`
	Error   *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

type walletClient struct {
	baseURL    string
	serviceKey string
	http       *http.Client
}

func (c *walletClient) do(ctx context.Context, method, path string, body, out any) error {
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(data)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
	if err != nil {
		return err
	}
	req.Header.Set("X-Service-Key", c.serviceKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	var res apiResponse
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return fmt.Errorf("%s %s: status %d, decode response: %w", method, path, resp.StatusCode, err)
	}

	if !res.Success {
		if res.Error != nil {
			return fmt.Errorf("%s %s: %d %s: %s", method, path, resp.StatusCode, res.Error.Code, res.Error.Message)
		}
		return fmt.Errorf("%s %s: status %d", method, path, resp.StatusCode)
	}

	if out == nil {
		return nil
	}

	return json.Unmarshal(res.Data, out)
}

func main() {
	fs := flag.NewFlagSet("ethereum-gas-station-send", flag.ExitOnError)
	fs.Usage = func() { fmt.Fprint(os.Stderr, usage) }
	to := fs.String("to", "", "")
	amount := fs.String("amount", "", "")
	purpose := fs.String("purpose", "gas_station", "")
	label := fs.String("label", "", "")
	send := fs.Bool("send", false, "")
	apiURL := fs.String("api", envOr("WALLET_API_URL", "http://localhost:8080"), "")
	serviceKey := fs.String("service-key", os.Getenv("WALLET_SERVICE_KEY"), "")
	rpcURL := fs.String("rpc", os.Getenv("ETH_RPC_URL"), "")
	_ = fs.Parse(os.Args[1:])

	if !common.IsHexAddress(*to) || *amount == "" || *serviceKey == "" || *rpcURL == "" {
		fs.Usage()
		os.Exit(2)
	}

	value, err := ethToWei(*amount)
	if err != nil {
		log.Fatalf("amount: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	wallet := &walletClient{baseURL: strings.TrimRight(*apiURL, "/"), serviceKey: *serviceKey, http: &http.Client{Timeout: 15 * time.Second}}

	// active key signs only with lease: other instances do not sign with it until release, nonce is not reused
	var lease struct {
		LeaseID string `json:"lease_id"`
		Key     struct {
			ID      string `json:"id"`
			Address string `json:"address"`
		} `json:"key"`
	}
	leaseReq := map[string]string{"network": "ethereum", "label": *label, "owner": "ethereum-gas-station-send"}
	if err := wallet.do(ctx, http.MethodPost, "/api/v1/roles/"+url.PathEscape(*purpose)+"/lease", leaseReq, &lease); err != nil {
		log.Fatalf("lease gas station key: %v", err)
	}
	// on fatal exit lease is freed by ttl
	defer func() {
		if err := wallet.do(ctx, http.MethodDelete, "/api/v1/leases/"+lease.LeaseID, nil, nil); err != nil {
			log.Printf("release lease: %v", err)
		}
	}()
	key := lease.Key
	fmt.Printf("gas station:  %s (key %s)\n", key.Address, key.ID)

	eth, err := ethclient.DialContext(ctx, *rpcURL)
	if err != nil {
		log.Fatalf("dial rpc: %v", err)
	}
	defer eth.Close()

	from := common.HexToAddress(key.Address)
	chainID, err := eth.ChainID(ctx)
	if err != nil {
		log.Fatalf("chain id: %v", err)
	}

	nonce, err := eth.PendingNonceAt(ctx, from)
	if err != nil {
		log.Fatalf("nonce: %v", err)
	}

	tip, err := eth.SuggestGasTipCap(ctx)
	if err != nil {
		log.Fatalf("gas tip: %v", err)
	}

	head, err := eth.HeaderByNumber(ctx, nil)
	if err != nil {
		log.Fatalf("latest header: %v", err)
	}
	feeCap := new(big.Int).Add(new(big.Int).Mul(head.BaseFee, big.NewInt(2)), tip)

	// transfer to new account costs more than 21000 gas
	toAddress := common.HexToAddress(*to)
	gas, err := eth.EstimateGas(ctx, ethereum.CallMsg{From: from, To: &toAddress, Value: value})
	if err != nil {
		log.Fatalf("estimate gas: %v", err)
	}

	tx := types.NewTx(&types.DynamicFeeTx{
		ChainID:   chainID,
		Nonce:     nonce,
		To:        &toAddress,
		Value:     value,
		Gas:       gas,
		GasTipCap: tip,
		GasFeeCap: feeCap,
	})

	rawTx, err := tx.MarshalBinary()
	if err != nil {
		log.Fatalf("encode tx: %v", err)
	}
	fmt.Printf("tx:           %s ETH to %s, nonce %d, chain %s, gas %d, max fee %s wei\n", *amount, toAddress.Hex(), nonce, chainID, gas, feeCap)

	var signed struct {
		SignedTx string `json:"signed_tx"`
		TxHash   string `json:"tx_hash"`
	}
	signReq := map[string]string{"network": "ethereum", "lease_id": lease.LeaseID, "raw_tx": "0x" + hex.EncodeToString(rawTx)}
	if err := wallet.do(ctx, http.MethodPost, "/api/v1/keys/"+key.ID+"/sign-transaction", signReq, &signed); err != nil {
		log.Fatalf("sign tx: %v", err)
	}
	fmt.Printf("tx hash:      %s\n", signed.TxHash)

	if !*send {
		fmt.Printf("signed tx:    %s\n", signed.SignedTx)
		fmt.Println("not sent, add -send to broadcast")
		return
	}

	signedTx := new(types.Transaction)
	if err := signedTx.UnmarshalBinary(common.FromHex(signed.SignedTx)); err != nil {
		log.Fatalf("decode signed tx: %v", err)
	}

	if err := eth.SendTransaction(ctx, signedTx); err != nil {
		log.Fatalf("send tx: %v", err)
	}
	fmt.Println("sent")
}

func ethToWei(amount string) (*big.Int, error) {
	eth, ok := new(big.Rat).SetString(amount)
	if !ok || eth.Sign() <= 0 {
		return nil, fmt.Errorf("%q must be positive number", amount)
	}

	wei := new(big.Rat).Mul(eth, new(big.Rat).SetInt(big.NewInt(1_000_000_000_000_000_000)))
	if !wei.IsInt() {
		return nil, fmt.Errorf("%q has more than 18 decimals", amount)
	}

	return wei.Num(), nil
}

func envOr(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}

	return fallback
}
