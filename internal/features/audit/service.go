package audit

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"gitlab.tve-development.com/backend/gtoken/wallet-service/internal/infra/db"
)

type repository interface {
	CreateSigningRecord(ctx context.Context, tx db.DBTX, record *SigningRecord) error
}

type Service struct {
	repo repository
	txm  db.TxManager
}

func NewService(repo repository, txm db.TxManager) *Service {
	return &Service{repo: repo, txm: txm}
}

func (s *Service) RecordSigning(ctx context.Context, record *SigningRecord) error {
	id, err := uuid.NewV7()
	if err != nil {
		return fmt.Errorf("generate signing audit id: %w", err)
	}

	record.ID = id
	record.CreatedAt = time.Now().UTC()

	// record is written even if caller has gone
	return s.repo.CreateSigningRecord(context.WithoutCancel(ctx), s.txm.GetPool(), record)
}
