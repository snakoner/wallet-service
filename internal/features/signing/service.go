package signing

import (
	"context"
	"encoding/hex"
	"fmt"

	"github.com/google/uuid"

	"gitlab.tve-development.com/backend/gtoken/wallet-service/internal/constants"
	"gitlab.tve-development.com/backend/gtoken/wallet-service/internal/features/audit"
	keystorage "gitlab.tve-development.com/backend/gtoken/wallet-service/internal/features/key_storage"
	"gitlab.tve-development.com/backend/gtoken/wallet-service/internal/infra/apperror"
	"gitlab.tve-development.com/backend/gtoken/wallet-service/internal/infra/logger"
	"gitlab.tve-development.com/backend/gtoken/wallet-service/internal/policy"
)

type keyProvider interface {
	UseSigningKey(
		ctx context.Context,
		service string,
		keyID uuid.UUID,
		network constants.Network,
		leaseID *uuid.UUID,
		check func(key *keystorage.WalletKey) error,
		fn func(key *keystorage.WalletKey, privateKey []byte) error,
	) error
	IsServiceAddress(ctx context.Context, service string, network constants.Network, address, purpose string) (bool, error)
}

type auditor interface {
	RecordSigning(ctx context.Context, record *audit.SigningRecord) error
}

type TxSigner interface {
	DecodeTx(rawTx []byte) (*policy.Operation, error)
	SignTx(rawTx []byte, privateKey []byte) ([]byte, string, error)
}

type Service struct {
	log     *logger.Logger
	keys    keyProvider
	audit   auditor
	signers map[constants.Network]TxSigner
}

func NewService(log *logger.Logger, keys keyProvider, audit auditor, signers map[constants.Network]TxSigner) *Service {
	return &Service{
		log:     log,
		keys:    keys,
		audit:   audit,
		signers: signers,
	}
}

// SignTransaction records every attempt; signature without audit record is not returned.
func (s *Service) SignTransaction(ctx context.Context, params SignTransactionParams) (*SignedTransaction, error) {
	signed, err := s.signTransaction(ctx, params)

	record := &audit.SigningRecord{
		ServiceClientID: params.ServiceClientID,
		KeyID:           params.KeyID,
		Network:         params.Network,
		RawTx:           params.RawTx,
	}
	if err == nil {
		record.TxHash = &signed.TxHash
	} else {
		code := apperror.CodeInternal
		if ae := apperror.FromError(err); ae != nil {
			code = ae.Code
		}
		record.ErrorCode = &code
	}

	if auditErr := s.audit.RecordSigning(ctx, record); auditErr != nil {
		if err != nil {
			s.log.Error().Err(auditErr).Str("key_id", params.KeyID.String()).Msg("record signing audit")
			return nil, err
		}

		return nil, fmt.Errorf("record signing audit: %w", auditErr)
	}

	return signed, err
}

func (s *Service) signTransaction(ctx context.Context, params SignTransactionParams) (*SignedTransaction, error) {
	signer, ok := s.signers[params.Network]
	if !ok {
		return nil, keystorage.ErrUnsupportedNetwork
	}

	op, err := signer.DecodeTx(params.RawTx)
	if err != nil {
		return nil, ErrInvalidTransaction.WithMessage("invalid transaction: " + err.Error()).WithCause(err)
	}

	var (
		signed SignedTransaction
		rule   int
	)
	err = s.keys.UseSigningKey(ctx, params.Service, params.KeyID, params.Network, params.LeaseID,
		func(key *keystorage.WalletKey) error {
			rule, err = s.checkPolicy(ctx, key, op)
			return err
		},
		func(key *keystorage.WalletKey, privateKey []byte) error {
			signedTx, txHash, err := signer.SignTx(params.RawTx, privateKey)
			if err != nil {
				return fmt.Errorf("sign tx: %w", err)
			}

			signed = SignedTransaction{SignedTx: signedTx, TxHash: txHash}

			s.log.Info().
				Str("key_id", key.ID.String()).
				Str("service", key.Role.Service).
				Str("purpose", key.Role.Purpose).
				Str("label", key.Role.Label).
				Str("network", key.Network.String()).
				Str("address", key.Address).
				Str("op", string(op.Op)).
				Int("policy_rule", rule).
				Str("tx_hash", txHash).
				Msg("transaction signed")

			return nil
		},
	)
	if err != nil {
		return nil, err
	}

	return &signed, nil
}

const replacementRule = -1

func (s *Service) checkPolicy(ctx context.Context, key *keystorage.WalletKey, op *policy.Operation) (int, error) {
	if op.From != "" && op.From != key.Address {
		return 0, ErrInvalidTransaction.WithMessage("tx owner is not the key address")
	}

	// replaced key may send funds to its replacement regardless of role policy
	if key.ReplacementAddress != nil && (op.Op == policy.OpNative || op.Op == policy.OpToken) && op.To == *key.ReplacementAddress {
		return replacementRule, nil
	}

	rolePolicy, err := policy.Parse(key.Role.Policy, key.Network)
	if err != nil {
		return 0, fmt.Errorf("parse role policy: %w", err)
	}

	rule, ok, err := rolePolicy.Allow(*op, func(address, purpose string) (bool, error) {
		return s.keys.IsServiceAddress(ctx, key.Role.Service, key.Network, address, purpose)
	})
	if err != nil {
		return 0, fmt.Errorf("check policy: %w", err)
	}

	if !ok {
		s.log.Warn().
			Str("key_id", key.ID.String()).
			Str("service", key.Role.Service).
			Str("purpose", key.Role.Purpose).
			Str("network", key.Network.String()).
			Str("op", string(op.Op)).
			Str("contract", op.Contract).
			Str("selector", hex.EncodeToString(op.Selector[:])).
			Str("to", op.To).
			Msg("transaction denied by policy")

		return 0, ErrPolicyViolation
	}

	return rule, nil
}
