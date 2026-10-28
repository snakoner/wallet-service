package main

import (
	"context"
	"encoding/hex"
	"flag"
	"fmt"
	"log"
	"math/big"
	"os"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"

	"gitlab.tve-development.com/backend/gtoken/wallet-service/pkg/client"
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

	wallet := client.New(*apiURL, *serviceKey, nil)

	// active key signs only with lease: other instances do not sign with it until release, nonce is not reused
	lease, err := wallet.AcquireLease(ctx, *purpose, client.LeaseRequest{Network: client.NetworkEthereum, Label: *label, Owner: "ethereum-gas-station-send"})
	if err != nil {
		log.Fatalf("lease gas station key: %v", err)
	}
	// on fatal exit lease is freed by ttl
	defer func() {
		if err := wallet.ReleaseLease(ctx, lease.LeaseID); err != nil {
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

	signed, err := wallet.SignTransaction(ctx, key.ID, client.SignRequest{Network: client.NetworkEthereum, RawTx: "0x" + hex.EncodeToString(rawTx), LeaseID: &lease.LeaseID})
	if err != nil {
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
