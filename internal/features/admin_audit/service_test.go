package admin_audit

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	"gitlab.tve-development.com/backend/gtoken/wallet-service/internal/infra/db"
)

type repoStub struct {
	records []*Record
	filter  ListFilter
}

func (r *repoStub) CreateRecord(_ context.Context, _ db.DBTX, record *Record) error {
	r.records = append(r.records, record)
	return nil
}

func (r *repoStub) ListRecords(_ context.Context, _ db.DBTX, filter ListFilter) ([]Record, error) {
	r.filter = filter
	return []Record{{ID: uuid.New()}}, nil
}

func (r *repoStub) CountRecords(_ context.Context, _ db.DBTX, _ ListFilter) (int64, error) {
	return 7, nil
}

type txmStub struct{}

func (txmStub) WithTx(ctx context.Context, fn func(ctx context.Context, tx db.DBTX) error) error {
	return fn(ctx, nil)
}

func (txmStub) GetPool() db.DBTX { return nil }

func TestWithActor(t *testing.T) {
	if _, _, ok := ActorFrom(context.Background()); ok {
		t.Fatal("actor without WithActor")
	}
	if _, _, ok := ActorFrom(WithActor(context.Background(), "  ", SourceCLI)); ok {
		t.Fatal("blank actor is set")
	}

	name, source, ok := ActorFrom(WithActor(context.Background(), " ivanov ", SourceCLI))
	if !ok || name != "ivanov" || source != SourceCLI {
		t.Fatalf("actor = %q, %q, %v", name, source, ok)
	}

	// cut by runes to column size
	name, _, _ = ActorFrom(WithActor(context.Background(), strings.Repeat("я", 200), SourceAPI))
	if len([]rune(name)) != maxActorLength || !strings.HasPrefix(name, "яя") {
		t.Fatalf("long actor = %q", name)
	}
}

func TestRecord(t *testing.T) {
	repo := &repoStub{}
	srv := NewService(repo, txmStub{})
	keyID := uuid.New()

	if err := srv.Record(context.Background(), nil, &Record{Action: ActionKeyRevoke, KeyID: &keyID}); !errors.Is(err, ErrNoActor) || len(repo.records) != 0 {
		t.Fatalf("without actor: err = %v", err)
	}

	ctx := WithActor(context.Background(), "ivanov", SourceAPI)
	if err := srv.Record(ctx, nil, &Record{Action: ActionKeyRevoke, Service: "exchange", KeyID: &keyID}); err != nil {
		t.Fatal(err)
	}
	r := repo.records[0]
	if r.ID == uuid.Nil || r.CreatedAt.IsZero() || r.Actor != "ivanov" || r.Source != SourceAPI || r.Action != ActionKeyRevoke || *r.KeyID != keyID || r.Details == nil {
		t.Fatalf("record = %+v", r)
	}

	records, total, err := srv.List(ctx, ListFilter{Actor: "ivanov", Limit: 10})
	if err != nil || len(records) != 1 || total != 7 || repo.filter.Actor != "ivanov" {
		t.Fatalf("list = %+v, %d, %v", records, total, err)
	}
}
