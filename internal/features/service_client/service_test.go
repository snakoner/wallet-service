package service_client

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"gitlab.tve-development.com/backend/gtoken/wallet-service/internal/infra/db"
	"gitlab.tve-development.com/backend/gtoken/wallet-service/internal/infra/logger"
)

// repoStub keeps clients in memory and checks one primary key per service like db index.
type repoStub struct {
	clients []*ServiceClient
}

func (r *repoStub) find(id uuid.UUID) *ServiceClient {
	for _, c := range r.clients {
		if c.ID == id {
			return c
		}
	}
	return nil
}

func (r *repoStub) CreateClient(_ context.Context, _ db.DBTX, client *ServiceClient) error {
	for _, c := range r.clients {
		if c.Service == client.Service && c.IsActive && c.ExpiresAt == nil {
			return ErrServiceClientExists
		}
	}
	r.clients = append(r.clients, client)
	return nil
}

func (r *repoStub) GetActiveByKeyHash(_ context.Context, _ db.DBTX, keyHash []byte, now time.Time) (*ServiceClient, error) {
	for _, c := range r.clients {
		if c.IsActive && bytes.Equal(c.KeyHash, keyHash) && (c.ExpiresAt == nil || c.ExpiresAt.After(now)) {
			return c, nil
		}
	}
	return nil, ErrUnauthorized
}

func (r *repoStub) GetPrimaryForUpdate(_ context.Context, _ db.DBTX, service string) (*ServiceClient, error) {
	for _, c := range r.clients {
		if c.Service == service && c.IsActive && c.ExpiresAt == nil {
			return c, nil
		}
	}
	return nil, ErrServiceClientNotFound
}

func (r *repoStub) ExpireClient(_ context.Context, _ db.DBTX, id uuid.UUID, expiresAt time.Time) error {
	r.find(id).ExpiresAt = &expiresAt
	return nil
}

func (r *repoStub) SetReplacedBy(_ context.Context, _ db.DBTX, id, replacedBy uuid.UUID) error {
	r.find(id).ReplacedBy = &replacedBy
	return nil
}

func (r *repoStub) RevokeClient(_ context.Context, _ db.DBTX, id uuid.UUID, revokedAt time.Time) error {
	c := r.find(id)
	if c == nil || !c.IsActive {
		return ErrServiceClientNotFound
	}
	c.IsActive, c.RevokedAt = false, &revokedAt
	return nil
}

func (r *repoStub) ListClients(_ context.Context, _ db.DBTX, service string) ([]ServiceClient, error) {
	clients := []ServiceClient{}
	for _, c := range r.clients {
		if service == "" || c.Service == service {
			clients = append(clients, *c)
		}
	}
	return clients, nil
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

func TestCreateClientExists(t *testing.T) {
	srv := newTestService(&repoStub{})
	if _, _, err := srv.CreateClient(context.Background(), "exchange"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := srv.CreateClient(context.Background(), "exchange"); !errors.Is(err, ErrServiceClientExists) {
		t.Fatalf("second create: err = %v", err)
	}
	if _, _, err := srv.CreateClient(context.Background(), "banknote"); err != nil {
		t.Fatalf("other service: err = %v", err)
	}
}

func TestRotateClient(t *testing.T) {
	repo := &repoStub{}
	srv := newTestService(repo)
	ctx := context.Background()

	oldKey, oldClient, err := srv.CreateClient(ctx, "exchange")
	if err != nil {
		t.Fatal(err)
	}

	newKey, client, old, err := srv.RotateClient(ctx, "exchange", time.Hour)
	if err != nil {
		t.Fatalf("RotateClient() error = %v", err)
	}
	if old.ID != oldClient.ID || *old.ReplacedBy != client.ID || client.ExpiresAt != nil || !old.ExpiresAt.After(time.Now().Add(59*time.Minute)) {
		t.Fatalf("old = %+v, new = %+v", old, client)
	}

	for name, key := range map[string]string{"new key": newKey, "old key in grace": oldKey} {
		if _, err := srv.Authenticate(ctx, key); err != nil {
			t.Fatalf("%s: err = %v", name, err)
		}
	}

	past := time.Now().Add(-time.Second)
	old.ExpiresAt = &past
	if _, err := srv.Authenticate(ctx, oldKey); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("expired old key: err = %v", err)
	}

	// rotation chain: new key becomes old one
	if _, _, second, err := srv.RotateClient(ctx, "exchange", 0); err != nil || second.ID != client.ID {
		t.Fatalf("second rotation: old = %+v, err = %v", second, err)
	}
	if _, err := srv.Authenticate(ctx, newKey); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("grace 0 must reject old key at once: err = %v", err)
	}
}

func TestRotateClientErrors(t *testing.T) {
	srv := newTestService(&repoStub{})
	ctx := context.Background()

	if _, _, _, err := srv.RotateClient(ctx, "exchange", time.Hour); !errors.Is(err, ErrServiceClientNotFound) {
		t.Fatalf("no primary: err = %v", err)
	}
	if _, _, _, err := srv.RotateClient(ctx, "exchange", -time.Second); !errors.Is(err, ErrInvalidGrace) {
		t.Fatalf("negative grace: err = %v", err)
	}
	if _, _, _, err := srv.RotateClient(ctx, " ", time.Hour); !errors.Is(err, ErrInvalidService) {
		t.Fatalf("empty service: err = %v", err)
	}
}

func TestRevokeAndListClients(t *testing.T) {
	srv := newTestService(&repoStub{})
	ctx := context.Background()

	key, client, err := srv.CreateClient(ctx, "exchange")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := srv.CreateClient(ctx, "banknote"); err != nil {
		t.Fatal(err)
	}

	if err := srv.RevokeClient(ctx, client.ID); err != nil {
		t.Fatalf("RevokeClient() error = %v", err)
	}
	if _, err := srv.Authenticate(ctx, key); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("revoked key: err = %v", err)
	}
	if err := srv.RevokeClient(ctx, client.ID); !errors.Is(err, ErrServiceClientNotFound) {
		t.Fatalf("revoke twice: err = %v", err)
	}

	// revoked primary frees the slot for a new key
	if _, _, err := srv.CreateClient(ctx, "exchange"); err != nil {
		t.Fatalf("create after revoke: err = %v", err)
	}

	if clients, err := srv.ListClients(ctx, "exchange"); err != nil || len(clients) != 2 {
		t.Fatalf("exchange clients = %d, err = %v", len(clients), err)
	}
	if clients, _ := srv.ListClients(ctx, ""); len(clients) != 3 {
		t.Fatalf("all clients = %d", len(clients))
	}
}
