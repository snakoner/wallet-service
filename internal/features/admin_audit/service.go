package admin_audit

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"gitlab.tve-development.com/backend/gtoken/wallet-service/internal/infra/db"
)

type repository interface {
	CreateRecord(ctx context.Context, tx db.DBTX, record *Record) error
	ListRecords(ctx context.Context, tx db.DBTX, filter ListFilter) ([]Record, error)
	CountRecords(ctx context.Context, tx db.DBTX, filter ListFilter) (int64, error)
}

type Service struct {
	repo repository
	txm  db.TxManager
}

func NewService(repo repository, txm db.TxManager) *Service {
	return &Service{repo: repo, txm: txm}
}

func (s *Service) Record(ctx context.Context, tx db.DBTX, record *Record) error {
	name, source, ok := ActorFrom(ctx)
	if !ok {
		return ErrNoActor
	}

	id, err := uuid.NewV7()
	if err != nil {
		return fmt.Errorf("generate admin audit id: %w", err)
	}

	record.ID = id
	record.CreatedAt = time.Now().UTC()
	record.Actor = name
	record.Source = source
	if record.Details == nil {
		record.Details = map[string]any{}
	}

	return s.repo.CreateRecord(ctx, tx, record)
}

func (s *Service) List(ctx context.Context, filter ListFilter) ([]Record, int64, error) {
	records, err := s.repo.ListRecords(ctx, s.txm.GetPool(), filter)
	if err != nil {
		return nil, 0, err
	}

	total, err := s.repo.CountRecords(ctx, s.txm.GetPool(), filter)
	if err != nil {
		return nil, 0, err
	}

	return records, total, nil
}
