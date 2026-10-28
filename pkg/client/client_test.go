package client

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
)

type request struct {
	method, uri, serviceKey string
	body                    map[string]any
}

func server(t *testing.T, status int, body string, header map[string]string) (*Client, *request) {
	t.Helper()
	last := &request{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*last = request{method: r.Method, uri: r.URL.RequestURI(), serviceKey: r.Header.Get("X-Service-Key")}
		if data, _ := io.ReadAll(r.Body); len(data) > 0 {
			if err := json.Unmarshal(data, &last.body); err != nil {
				t.Errorf("request body: %v", err)
			}
		}
		for name, value := range header {
			w.Header().Set(name, value)
		}
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)

	return New(srv.URL+"/", "secret", nil), last
}

func TestRequests(t *testing.T) {
	ctx := context.Background()
	keyID, leaseID := uuid.New(), uuid.New()
	key := `{"id":"` + keyID.String() + `","purpose":"gas station","network":"tron","kind":"single","address":"T1","status":"active","created_at":"2026-10-28T10:00:00Z"}`
	ok := func(data string) string { return `{"success":true,"data":` + data + `}` }

	c, last := server(t, http.StatusOK, ok(key), nil)
	got, err := c.GetKey(ctx, "gas station", "a&b", NetworkTron)
	if err != nil || got.ID != keyID || got.Address != "T1" || got.Status != StatusActive {
		t.Fatalf("GetKey = %+v, %v", got, err)
	}
	if last.method != http.MethodGet || last.uri != "/api/v1/roles/gas%20station/key?label=a%26b&network=tron" || last.serviceKey != "secret" {
		t.Fatalf("GetKey request = %+v", last)
	}

	c, last = server(t, http.StatusOK, ok("["+key+","+key+"]"), nil)
	keys, err := c.ListKeys(ctx, "executor", "", NetworkEthereum)
	if err != nil || len(keys) != 2 {
		t.Fatalf("ListKeys = %+v, %v", keys, err)
	}
	if last.uri != "/api/v1/roles/executor/keys?label=&network=ethereum" {
		t.Fatalf("ListKeys request = %+v", last)
	}

	c, last = server(t, http.StatusOK, ok(key), nil)
	if _, err := c.DepositKey(ctx, "deposit", DepositKeyRequest{Network: NetworkEthereum, ExternalRef: "order-1"}); err != nil {
		t.Fatal(err)
	}
	if last.method != http.MethodPost || last.uri != "/api/v1/roles/deposit/deposit-key" || last.body["external_ref"] != "order-1" || last.body["network"] != "ethereum" {
		t.Fatalf("DepositKey request = %+v", last)
	}
	if _, ok := last.body["alias"]; ok {
		t.Fatalf("DepositKey sends empty alias: %+v", last.body)
	}

	c, last = server(t, http.StatusOK, ok(`{"lease_id":"`+leaseID.String()+`","expires_at":"2026-10-28T10:01:00Z","key":`+key+`}`), nil)
	lease, err := c.AcquireLease(ctx, "executor", LeaseRequest{Network: NetworkEthereum, Owner: "pod-1", TTLSeconds: 120, Exclude: []uuid.UUID{keyID}})
	if err != nil || lease.LeaseID != leaseID || lease.Key.ID != keyID || lease.ExpiresAt.IsZero() {
		t.Fatalf("AcquireLease = %+v, %v", lease, err)
	}
	if last.uri != "/api/v1/roles/executor/lease" || last.body["owner"] != "pod-1" || last.body["ttl_seconds"] != float64(120) || len(last.body["exclude"].([]any)) != 1 {
		t.Fatalf("AcquireLease request = %+v", last)
	}

	c, last = server(t, http.StatusOK, ok(`{"expires_at":"2026-10-28T10:02:00Z"}`), nil)
	expiresAt, err := c.ExtendLease(ctx, leaseID, 0)
	if err != nil || !expiresAt.Equal(time.Date(2026, 10, 28, 10, 2, 0, 0, time.UTC)) {
		t.Fatalf("ExtendLease = %v, %v", expiresAt, err)
	}
	if last.uri != "/api/v1/leases/"+leaseID.String()+"/extend" || len(last.body) != 0 {
		t.Fatalf("ExtendLease request = %+v", last)
	}

	c, last = server(t, http.StatusOK, `{"success":true}`, nil)
	if err := c.ReleaseLease(ctx, leaseID); err != nil {
		t.Fatal(err)
	}
	if last.method != http.MethodDelete || last.uri != "/api/v1/leases/"+leaseID.String() {
		t.Fatalf("ReleaseLease request = %+v", last)
	}

	c, last = server(t, http.StatusOK, ok(`{"signed_tx":"0xab","tx_hash":"0xcd"}`), nil)
	signed, err := c.SignTransaction(ctx, keyID, SignRequest{Network: NetworkEthereum, RawTx: "0x01", LeaseID: &leaseID})
	if err != nil || signed.SignedTx != "0xab" || signed.TxHash != "0xcd" {
		t.Fatalf("SignTransaction = %+v, %v", signed, err)
	}
	if last.uri != "/api/v1/keys/"+keyID.String()+"/sign-transaction" || last.body["raw_tx"] != "0x01" || last.body["lease_id"] != leaseID.String() {
		t.Fatalf("SignTransaction request = %+v", last)
	}
}

func TestErrors(t *testing.T) {
	ctx := context.Background()

	c, _ := server(t, http.StatusConflict, `{"success":false,"error":{"code":"NO_FREE_KEY","message":"no free key in role, retry later"}}`, map[string]string{"Retry-After": "3"})
	_, err := c.AcquireLease(ctx, "executor", LeaseRequest{Network: NetworkEthereum})
	var apiErr *Error
	if !errors.Is(err, ErrNoFreeKey) || errors.Is(err, ErrLeaseExpired) || !errors.As(err, &apiErr) {
		t.Fatalf("err = %v", err)
	}
	if apiErr.Status != http.StatusConflict || apiErr.RetryAfter != 3*time.Second || apiErr.Message == "" {
		t.Fatalf("api error = %+v", apiErr)
	}

	c, _ = server(t, http.StatusBadRequest, `{"success":false,"error":{"code":"VALIDATION_FAILED","message":"validation failed","fields":{"network":"unsupported network"}}}`, nil)
	_, err = c.GetKey(ctx, "gas_station", "", "bitcoin")
	if !errors.As(err, &apiErr) || !errors.Is(err, ErrValidationFailed) || apiErr.Fields["network"] != "unsupported network" {
		t.Fatalf("err = %v", err)
	}

	// proxy answers without wallet-service body
	c, _ = server(t, http.StatusBadGateway, `bad gateway`, nil)
	if _, err = c.GetKey(ctx, "gas_station", "", NetworkEthereum); err == nil || errors.As(err, &apiErr) {
		t.Fatalf("not json: err = %v", err)
	}

	c, _ = server(t, http.StatusInternalServerError, `{"success":false}`, nil)
	err = c.ReleaseLease(ctx, uuid.New())
	if !errors.As(err, &apiErr) || apiErr.Status != http.StatusInternalServerError || errors.Is(err, &Error{}) {
		t.Fatalf("no error body: err = %v", err)
	}
}
