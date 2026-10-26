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

	tronlib "github.com/w3-developers/go-tron-lib"
)

const usage = `tron-gas-station-send - send TRX from gas station via wallet service

Usage:
  tron-gas-station-send -to <address> -amount <trx> [flags]

Flags:
  -to <address>        recipient, base58 T...
  -amount <trx>        amount in TRX, e.g. 1.5
  -purpose <purpose>   gas station role purpose (default gas_station)
  -label <label>       gas station label, e.g. atoken
  -send                broadcast signed tx, otherwise only print it
  -api <url>           wallet service url (env WALLET_API_URL, default http://localhost:8080)
  -service-key <key>   X-Service-Key (env WALLET_SERVICE_KEY)
  -node <url>          tron http api, e.g. https://nile.trongrid.io (env TRON_NODE_URL)
  -node-key <key>      TronGrid api key (env TRON_API_KEY)

Example:
  WALLET_SERVICE_KEY=... TRON_NODE_URL=https://nile.trongrid.io tron-gas-station-send -to T... -amount 1 -send
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

	return json.Unmarshal(res.Data, out)
}

type tronNode struct {
	baseURL string
	apiKey  string
	http    *http.Client
}

func (n *tronNode) post(ctx context.Context, path string, body, out any) error {
	data, err := json.Marshal(body)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, n.baseURL+path, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if n.apiKey != "" {
		req.Header.Set("TRON-PRO-API-KEY", n.apiKey)
	}

	resp, err := n.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s: status %d: %s", path, resp.StatusCode, raw)
	}

	return json.Unmarshal(raw, out)
}

func main() {
	fs := flag.NewFlagSet("tron-gas-station-send", flag.ExitOnError)
	fs.Usage = func() { fmt.Fprint(os.Stderr, usage) }
	to := fs.String("to", "", "")
	amount := fs.String("amount", "", "")
	purpose := fs.String("purpose", "gas_station", "")
	label := fs.String("label", "", "")
	send := fs.Bool("send", false, "")
	apiURL := fs.String("api", envOr("WALLET_API_URL", "http://localhost:8080"), "")
	serviceKey := fs.String("service-key", os.Getenv("WALLET_SERVICE_KEY"), "")
	nodeURL := fs.String("node", os.Getenv("TRON_NODE_URL"), "")
	nodeKey := fs.String("node-key", os.Getenv("TRON_API_KEY"), "")
	_ = fs.Parse(os.Args[1:])

	if *to == "" || *amount == "" || *serviceKey == "" || *nodeURL == "" {
		fs.Usage()
		os.Exit(2)
	}

	if err := tronlib.ValidateBase58Address(*to); err != nil {
		log.Fatalf("to: %v", err)
	}

	value, err := trxToSun(*amount)
	if err != nil {
		log.Fatalf("amount: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	httpClient := &http.Client{Timeout: 15 * time.Second}
	wallet := &walletClient{baseURL: strings.TrimRight(*apiURL, "/"), serviceKey: *serviceKey, http: httpClient}
	node := &tronNode{baseURL: strings.TrimRight(*nodeURL, "/"), apiKey: *nodeKey, http: httpClient}

	var key struct {
		ID      string `json:"id"`
		Address string `json:"address"`
	}
	query := url.Values{"network": {"tron"}, "label": {*label}}
	if err := wallet.do(ctx, http.MethodGet, "/api/v1/roles/"+url.PathEscape(*purpose)+"/key?"+query.Encode(), nil, &key); err != nil {
		log.Fatalf("get gas station key: %v", err)
	}
	fmt.Printf("gas station:  %s (key %s)\n", key.Address, key.ID)

	var tx struct {
		TxID       string `json:"txID"`
		RawDataHex string `json:"raw_data_hex"`
		Error      string `json:"Error"`
	}
	createReq := map[string]any{"owner_address": key.Address, "to_address": *to, "amount": value, "visible": true}
	if err := node.post(ctx, "/wallet/createtransaction", createReq, &tx); err != nil {
		log.Fatalf("create tx: %v", err)
	}
	if tx.Error != "" || tx.RawDataHex == "" {
		log.Fatalf("create tx: %s", tx.Error)
	}
	fmt.Printf("tx:           %s TRX to %s (%d sun)\n", *amount, *to, value)

	var signed struct {
		SignedTx string `json:"signed_tx"`
		TxHash   string `json:"tx_hash"`
	}
	signReq := map[string]string{"network": "tron", "raw_tx": tx.RawDataHex}
	if err := wallet.do(ctx, http.MethodPost, "/api/v1/keys/"+key.ID+"/sign-transaction", signReq, &signed); err != nil {
		log.Fatalf("sign tx: %v", err)
	}
	if signed.TxHash != tx.TxID {
		log.Fatalf("tx hash mismatch: signed %s, node %s", signed.TxHash, tx.TxID)
	}
	fmt.Printf("tx hash:      %s\n", signed.TxHash)

	if !*send {
		fmt.Printf("signed tx:    %s\n", signed.SignedTx)
		fmt.Println("not sent, add -send to broadcast")
		return
	}

	var result struct {
		Result  bool   `json:"result"`
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	if err := node.post(ctx, "/wallet/broadcasthex", map[string]string{"transaction": signed.SignedTx}, &result); err != nil {
		log.Fatalf("broadcast tx: %v", err)
	}
	if !result.Result {
		message, err := hex.DecodeString(result.Message)
		if err != nil {
			message = []byte(result.Message)
		}
		log.Fatalf("broadcast tx: %s: %s", result.Code, message)
	}
	fmt.Println("sent")
}

func trxToSun(amount string) (int64, error) {
	trx, ok := new(big.Rat).SetString(amount)
	if !ok || trx.Sign() <= 0 {
		return 0, fmt.Errorf("%q must be positive number", amount)
	}

	sun := new(big.Rat).Mul(trx, new(big.Rat).SetInt64(1_000_000))
	if !sun.IsInt() {
		return 0, fmt.Errorf("%q has more than 6 decimals", amount)
	}

	if !sun.Num().IsInt64() {
		return 0, fmt.Errorf("%q is too large", amount)
	}

	return sun.Num().Int64(), nil
}

func envOr(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}

	return fallback
}
