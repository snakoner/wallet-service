//go:build integration

package integration

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"

	"gitlab.tve-development.com/backend/gtoken/wallet-service/internal/constants"
)

// apiCall is e.call without t.Fatal: safe in goroutines.
func (e *env) apiCall(method, path string, body, out any) (int, apiResponse, error) {
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return 0, apiResponse{}, err
		}
		reader = bytes.NewReader(data)
	}

	req, err := http.NewRequestWithContext(e.ctx, method, e.apiURL+path, reader)
	if err != nil {
		return 0, apiResponse{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Service-Key", e.apiKey)

	resp, err := e.http.Do(req)
	if err != nil {
		return 0, apiResponse{}, err
	}
	defer resp.Body.Close()

	var res apiResponse
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return resp.StatusCode, res, fmt.Errorf("%s %s: decode: %w", method, path, err)
	}
	if out != nil && res.Success {
		if err := json.Unmarshal(res.Data, out); err != nil {
			return resp.StatusCode, res, err
		}
	}

	return resp.StatusCode, res, nil
}

// Two pods send gas from one single gas station key at the same time: each tx is sent under lease,
// pods wait for each other and never get the same nonce.
func TestParallelGasStationPods(t *testing.T) {
	const txPerPod = 5

	e := newEnv(t)
	gas := e.key(e.role("gas_station", constants.NetworkEthereum, constants.RoleKindSingle, gasStationPolicy), "")
	deposit := e.key(e.role("deposit", constants.NetworkEthereum, constants.RoleKindDeposit, ""), "order-1")
	e.fund(gas.Address, oneEth)

	from, to := common.HexToAddress(gas.Address), common.HexToAddress(deposit.Address)
	gasPrice, err := e.eth.SuggestGasPrice(e.ctx)
	if err != nil {
		t.Fatal(err)
	}
	gasPrice.Mul(gasPrice, big.NewInt(2))

	var (
		inside atomic.Int32 // pods between lease and release
		busy   atomic.Int32 // NO_FREE_KEY answers: pods really competed
		mu     sync.Mutex
		nonces []uint64
		hashes []common.Hash
	)

	pod := func(name string) error {
		for i := range txPerPod {
			var lease leaseView
			for {
				status, res, err := e.apiCall(http.MethodPost, "/api/v1/roles/gas_station/lease", map[string]any{"network": "ethereum", "owner": name, "ttl_seconds": 30}, &lease)
				if err != nil {
					return err
				}
				if status == http.StatusOK {
					break
				}
				if res.Error.Code != "NO_FREE_KEY" {
					return fmt.Errorf("%s tx %d: lease: status = %d, error = %+v", name, i, status, res.Error)
				}
				busy.Add(1)
				time.Sleep(20 * time.Millisecond)
			}

			if n := inside.Add(1); n != 1 {
				return fmt.Errorf("%s tx %d: %d pods hold gas station lease", name, i, n)
			}

			nonce, err := e.eth.PendingNonceAt(e.ctx, from)
			if err != nil {
				return err
			}
			raw, err := types.NewTransaction(nonce, to, big.NewInt(1), 21000, gasPrice, nil).MarshalBinary()
			if err != nil {
				return err
			}

			var out signed
			body := map[string]any{"network": "ethereum", "raw_tx": "0x" + common.Bytes2Hex(raw), "lease_id": lease.LeaseID.String()}
			status, res, err := e.apiCall(http.MethodPost, "/api/v1/keys/"+gas.ID.String()+"/sign-transaction", body, &out)
			if err != nil {
				return err
			}
			if status != http.StatusOK {
				return fmt.Errorf("%s tx %d: sign: status = %d, error = %+v", name, i, status, res.Error)
			}

			tx := new(types.Transaction)
			if err := tx.UnmarshalBinary(common.FromHex(out.SignedTx)); err != nil {
				return err
			}
			if err := e.eth.SendTransaction(e.ctx, tx); err != nil {
				return fmt.Errorf("%s tx %d: send nonce %d: %w", name, i, nonce, err)
			}

			mu.Lock()
			nonces, hashes = append(nonces, nonce), append(hashes, tx.Hash())
			mu.Unlock()

			// pod still works under lease, other pod must wait
			time.Sleep(30 * time.Millisecond)
			inside.Add(-1)

			if status, res, err := e.apiCall(http.MethodDelete, "/api/v1/leases/"+lease.LeaseID.String(), nil, nil); err != nil || status != http.StatusOK {
				return fmt.Errorf("%s tx %d: release: status = %d, error = %+v, err = %v", name, i, status, res.Error, err)
			}
		}

		return nil
	}

	errs := make(chan error, 2)
	for _, name := range []string{"pod-a", "pod-b"} {
		go func() { errs <- pod(name) }()
	}
	for range 2 {
		if err := <-errs; err != nil {
			t.Fatal(err)
		}
	}

	slices.Sort(nonces)
	for i, nonce := range nonces {
		if nonce != uint64(i) {
			t.Fatalf("nonces = %v, want 0..%d without gaps and repeats", nonces, 2*txPerPod-1)
		}
	}
	if len(nonces) != 2*txPerPod {
		t.Fatalf("sent %d tx, want %d", len(nonces), 2*txPerPod)
	}

	for _, hash := range hashes {
		e.waitReceipt(hash)
	}
	if mined, err := e.eth.NonceAt(e.ctx, from, nil); err != nil || mined != 2*txPerPod {
		t.Fatalf("mined nonce = %d, err = %v", mined, err)
	}

	if busy.Load() == 0 {
		t.Fatal("pods never competed for the key, test does not check anything")
	}
}

func (e *env) waitReceipt(hash common.Hash) {
	e.tb.Helper()
	for {
		receipt, err := e.eth.TransactionReceipt(e.ctx, hash)
		if err == nil {
			if receipt.Status != types.ReceiptStatusSuccessful {
				e.tb.Fatalf("tx %s reverted", hash)
			}
			return
		}

		select {
		case <-e.ctx.Done():
			e.tb.Fatalf("wait receipt %s: %v", hash, e.ctx.Err())
		case <-time.After(100 * time.Millisecond):
		}
	}
}
