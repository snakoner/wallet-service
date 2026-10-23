package service_client

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"gitlab.tve-development.com/backend/gtoken/wallet-service/internal/infra/db"
	"gitlab.tve-development.com/backend/gtoken/wallet-service/internal/infra/logger"
)

type repoStub struct {
	clients []*ServiceClient
}

func (r *repoStub) CreateClient(_ context.Context, _ db.DBTX, client *ServiceClient) error {
	r.clients = append(r.clients, client)
	return nil
}

func (r *repoStub) GetActiveByKeyHash(_ context.Context, _ db.DBTX, keyHash []byte) (*ServiceClient, error) {
	for _, c := range r.clients {
		if c.IsActive && bytes.Equal(c.KeyHash, keyHash) {
			return c, nil
		}
	}
	return nil, ErrUnauthorized
}

type txmStub struct{}

func (txmStub) WithTx(ctx context.Context, fn func(ctx context.Context, tx db.DBTX) error) error {
	return fn(ctx, nil)
}

func (txmStub) GetPool() db.DBTX { return nil }

func newTestService(repo repository) *Service {
	return NewService(logger.New(logger.Config{Level: "error"}), repo, txmStub{})
}

func TestCreateClientAndAuthenticate(t *testing.T) {
	repo := &repoStub{}
	srv := newTestService(repo)

	key, client, err := srv.CreateClient(context.Background(), " exchange ")
	if err != nil {
		t.Fatalf("CreateClient() error = %v", err)
	}
	if len(key) != 2*keySize || client.Service != "exchange" || !client.IsActive {
		t.Fatalf("key = %q, client = %+v", key, client)
	}
	if bytes.Contains(client.KeyHash, []byte(key)) || bytes.Equal(client.KeyHash, []byte(key)) {
		t.Fatal("key must be stored only as hash")
	}

	got, err := srv.Authenticate(context.Background(), key)
	if err != nil || got.ID != client.ID {
		t.Fatalf("Authenticate() = %+v, %v", got, err)
	}

	for _, bad := range []string{"", "wrong", key + "x"} {
		if _, err := srv.Authenticate(context.Background(), bad); !errors.Is(err, ErrUnauthorized) {
			t.Fatalf("Authenticate(%q) err = %v", bad, err)
		}
	}

	client.IsActive = false
	if _, err := srv.Authenticate(context.Background(), key); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("revoked client: err = %v", err)
	}
}

func TestCreateClientEmptyService(t *testing.T) {
	if _, _, err := newTestService(&repoStub{}).CreateClient(context.Background(), "  "); !errors.Is(err, ErrInvalidService) {
		t.Fatalf("err = %v", err)
	}
}
