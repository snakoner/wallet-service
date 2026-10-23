package signing

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"gitlab.tve-development.com/backend/gtoken/wallet-service/internal/app/middleware"
	"gitlab.tve-development.com/backend/gtoken/wallet-service/internal/constants"
	keystorage "gitlab.tve-development.com/backend/gtoken/wallet-service/internal/features/key_storage"
	"gitlab.tve-development.com/backend/gtoken/wallet-service/internal/infra/logger"
	"gitlab.tve-development.com/backend/gtoken/wallet-service/internal/validation"
)

type serviceStub struct {
	params SignTransactionParams
	err    error
}

func (s *serviceStub) SignTransaction(_ context.Context, params SignTransactionParams) (*SignedTransaction, error) {
	s.params = params
	if s.err != nil {
		return nil, s.err
	}
	return &SignedTransaction{SignedTx: []byte{0xab, 0xcd}, TxHash: "0xhash"}, nil
}

func doSignTransaction(t *testing.T, svc *serviceStub, service, keyID, body string) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	if err := validation.Register(); err != nil {
		t.Fatal(err)
	}
	r := gin.New()
	setService := func(c *gin.Context) {
		if service != "" {
			c.Set(middleware.ContextServiceKey, service)
		}
		c.Next()
	}
	r.POST("/api/v1/keys/:id/sign-transaction", setService, NewHandler(svc, logger.New(logger.Config{Level: "error"})).SignTransaction)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/v1/keys/"+keyID+"/sign-transaction", strings.NewReader(body)))
	return w
}

func TestHandlerSignTransaction(t *testing.T) {
	svc := &serviceStub{}
	keyID := uuid.New()
	w := doSignTransaction(t, svc, "exchange", keyID.String(), `{"network":"ethereum","raw_tx":"0x0102"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("code = %d, body = %s", w.Code, w.Body)
	}
	if svc.params.Service != "exchange" || svc.params.KeyID != keyID || svc.params.Network != constants.NetworkEthereum || string(svc.params.RawTx) != "\x01\x02" {
		t.Fatalf("params = %+v", svc.params)
	}
	var resp struct {
		Data map[string]any `json:"data"`
	}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Data["signed_tx"] != "0xabcd" || resp.Data["tx_hash"] != "0xhash" {
		t.Fatalf("data = %v", resp.Data)
	}
}

func TestHandlerSignTransactionErrors(t *testing.T) {
	keyID := uuid.New().String()
	cases := []struct {
		name    string
		service string
		keyID   string
		body    string
		err     error
		want    int
	}{
		{"no service in context", "", keyID, `{"network":"ethereum","raw_tx":"0x01"}`, nil, http.StatusUnauthorized},
		{"bad key id", "x", "abc", `{"network":"ethereum","raw_tx":"0x01"}`, nil, http.StatusNotFound},
		{"no raw tx", "x", keyID, `{"network":"ethereum"}`, nil, http.StatusBadRequest},
		{"no network", "x", keyID, `{"raw_tx":"0x01"}`, nil, http.StatusBadRequest},
		{"unknown network", "x", keyID, `{"network":"btc","raw_tx":"0x01"}`, nil, http.StatusBadRequest},
		{"bad hex", "x", keyID, `{"network":"ethereum","raw_tx":"0xzz"}`, nil, http.StatusBadRequest},
		{"key not found", "x", keyID, `{"network":"ethereum","raw_tx":"0x01"}`, keystorage.ErrKeyNotFound, http.StatusNotFound},
		{"key not active", "x", keyID, `{"network":"ethereum","raw_tx":"0x01"}`, keystorage.ErrKeyNotActive, http.StatusConflict},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if w := doSignTransaction(t, &serviceStub{err: tc.err}, tc.service, tc.keyID, tc.body); w.Code != tc.want {
				t.Fatalf("code = %d, want %d, body = %s", w.Code, tc.want, w.Body)
			}
		})
	}
}
