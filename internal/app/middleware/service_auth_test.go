package middleware

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	serviceclient "gitlab.tve-development.com/backend/gtoken/wallet-service/internal/features/service_client"
	"gitlab.tve-development.com/backend/gtoken/wallet-service/internal/infra/logger"
)

var stubClientID = uuid.New()

type authStub struct{ err error }

func (a authStub) Authenticate(_ context.Context, key string) (*serviceclient.ServiceClient, error) {
	if a.err != nil {
		return nil, a.err
	}
	if key != "good" {
		return nil, serviceclient.ErrUnauthorized
	}
	return &serviceclient.ServiceClient{ID: stubClientID, Service: "exchange"}, nil
}

func doRequest(t *testing.T, auth authStub, key string) (*httptest.ResponseRecorder, string, string) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	var service, headerAfter string
	var clientID uuid.UUID
	r := gin.New()
	r.GET("/", NewServiceAuth(auth, logger.New(logger.Config{Level: "error"})).Handle(), func(c *gin.Context) {
		service, _ = GetService(c)
		clientID, _ = GetServiceClientID(c)
		headerAfter = c.GetHeader(serviceKeyHeader)
		c.Status(http.StatusOK)
	})
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	if key != "" {
		req.Header.Set(serviceKeyHeader, key)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if clientID != uuid.Nil && clientID != stubClientID {
		t.Fatalf("service client id = %s, want %s", clientID, stubClientID)
	}
	return w, service, headerAfter
}

func TestServiceAuth(t *testing.T) {
	w, service, headerAfter := doRequest(t, authStub{}, "good")
	if w.Code != http.StatusOK || service != "exchange" || headerAfter != "" {
		t.Fatalf("code = %d, service = %q, header = %q", w.Code, service, headerAfter)
	}

	for _, key := range []string{"", "bad"} {
		if w, _, _ := doRequest(t, authStub{}, key); w.Code != http.StatusUnauthorized {
			t.Fatalf("key %q: code = %d", key, w.Code)
		}
	}

	if w, _, _ := doRequest(t, authStub{err: errors.New("db down")}, "good"); w.Code != http.StatusInternalServerError {
		t.Fatalf("db error: code = %d", w.Code)
	}
}
