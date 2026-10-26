package key_storage

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"gitlab.tve-development.com/backend/gtoken/wallet-service/internal/constants"
)

func TestHandlerGetRoleKey(t *testing.T) {
	svc := &serviceStub{}
	w := doGetRoleKey(t, svc, "exchange", "/api/v1/roles/gas_station/key?label=atoken&network=tron")
	if w.Code != http.StatusOK {
		t.Fatalf("code = %d, body = %s", w.Code, w.Body)
	}
	p := svc.params
	if p.Service != "exchange" || p.Purpose != "gas_station" || p.Label != "atoken" || p.Network != constants.NetworkTron {
		t.Fatalf("params = %+v", p)
	}
	var resp struct {
		Data map[string]any `json:"data"`
	}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Data["address"] != "TAddr" || resp.Data["network"] != "tron" || resp.Data["kind"] != "single" || resp.Data["label"] != "atoken" {
		t.Fatalf("data = %v", resp.Data)
	}
	if strings.Contains(w.Body.String(), "cipher") {
		t.Fatal("response must not contain ciphers")
	}
}

func TestHandlerGetRoleKeyErrors(t *testing.T) {
	cases := []struct {
		name    string
		service string
		path    string
		err     error
		want    int
	}{
		{"no service in context", "", "/api/v1/roles/gas_station/key?network=tron", nil, http.StatusUnauthorized},
		{"no network", "x", "/api/v1/roles/gas_station/key", nil, http.StatusBadRequest},
		{"unknown network", "x", "/api/v1/roles/gas_station/key?network=btc", nil, http.StatusBadRequest},
		{"role not found", "x", "/api/v1/roles/gas_station/key?network=tron", ErrRoleNotFound, http.StatusNotFound},
		{"no active key", "x", "/api/v1/roles/gas_station/key?network=tron", ErrKeyNotFound, http.StatusNotFound},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if w := doGetRoleKey(t, &serviceStub{err: tc.err}, tc.service, tc.path); w.Code != tc.want {
				t.Fatalf("code = %d, want %d, body = %s", w.Code, tc.want, w.Body)
			}
		})
	}
}

func TestHandlerGetRoleKeys(t *testing.T) {
	svc := &serviceStub{}
	w := doGetRoleKey(t, svc, "exchange", "/api/v1/roles/executor/keys?network=ethereum")
	if w.Code != http.StatusOK {
		t.Fatalf("code = %d, body = %s", w.Code, w.Body)
	}
	if p := svc.params; p.Service != "exchange" || p.Purpose != "executor" || p.Label != "" || p.Network != constants.NetworkEthereum {
		t.Fatalf("params = %+v", p)
	}
	var resp struct {
		Data []map[string]any `json:"data"`
	}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if len(resp.Data) != 2 || resp.Data[0]["address"] != "0xA" || resp.Data[1]["status"] != "rotating" || resp.Data[0]["kind"] != "pool" {
		t.Fatalf("data = %v", resp.Data)
	}
	if strings.Contains(w.Body.String(), "cipher") || strings.Contains(w.Body.String(), "secret") {
		t.Fatal("response must not contain ciphers")
	}

	for name, tc := range map[string]struct {
		service, path string
		err           error
		want          int
	}{
		"no service":     {"", "/api/v1/roles/executor/keys?network=ethereum", nil, http.StatusUnauthorized},
		"no network":     {"x", "/api/v1/roles/executor/keys", nil, http.StatusBadRequest},
		"role not found": {"x", "/api/v1/roles/executor/keys?network=ethereum", ErrRoleNotFound, http.StatusNotFound},
		"deposit role":   {"x", "/api/v1/roles/deposit/keys?network=ethereum", ErrInvalidParams, http.StatusBadRequest},
	} {
		t.Run(name, func(t *testing.T) {
			if w := doGetRoleKey(t, &serviceStub{err: tc.err}, tc.service, tc.path); w.Code != tc.want {
				t.Fatalf("code = %d, want %d, body = %s", w.Code, tc.want, w.Body)
			}
		})
	}
}

func TestHandlerGetDepositKey(t *testing.T) {
	svc := &serviceStub{}
	w := doGetRoleKey(t, svc, "exchange", `POST /api/v1/roles/deposit/deposit-key {"network":"tron","label":"usdt","external_ref":"order-1"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("code = %d, body = %s", w.Code, w.Body)
	}
	p := svc.deposit
	if p.Service != "exchange" || p.Purpose != "deposit" || p.Label != "usdt" || p.Network != constants.NetworkTron || p.ExternalRef != "order-1" {
		t.Fatalf("params = %+v", p)
	}
	var resp struct {
		Data map[string]any `json:"data"`
	}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Data["address"] != "TDeposit" || resp.Data["external_ref"] != "order-1" || resp.Data["kind"] != "deposit" {
		t.Fatalf("data = %v", resp.Data)
	}

	cases := map[string]struct {
		body string
		err  error
		want int
	}{
		"no external_ref": {`{"network":"tron"}`, nil, http.StatusBadRequest},
		"no network":      {`{"external_ref":"order-1"}`, nil, http.StatusBadRequest},
		"long ref":        {`{"network":"tron","external_ref":"` + strings.Repeat("a", 129) + `"}`, nil, http.StatusBadRequest},
		"role not found":  {`{"network":"tron","external_ref":"order-1"}`, ErrRoleNotFound, http.StatusNotFound},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			w := doGetRoleKey(t, &serviceStub{err: tc.err}, "exchange", "POST /api/v1/roles/deposit/deposit-key "+tc.body)
			if w.Code != tc.want {
				t.Fatalf("code = %d, want %d, body = %s", w.Code, tc.want, w.Body)
			}
		})
	}
}
