package signing

import (
	"context"
	"encoding/hex"
	"fmt"

	"github.com/google/uuid"

	"gitlab.tve-development.com/backend/gtoken/wallet-service/internal/constants"
	keystorage "gitlab.tve-development.com/backend/gtoken/wallet-service/internal/features/key_storage"
	"gitlab.tve-development.com/backend/gtoken/wallet-service/internal/infra/logger"
	"gitlab.tve-development.com/backend/gtoken/wallet-service/internal/policy"
)

type keyProvider interface {
	UseSigningKey(
		ctx context.Context,
		service string,
		keyID uuid.UUID,
		network constants.Network,
		check func(key *keystorage.WalletKey) error,
		fn func(key *keystorage.WalletKey, privateKey []byte) error,
	) error
	IsServiceAddress(ctx context.Context, service string, network constants.Network, address, purpose string) (bool, error)
}

type TxSigner interface {
	DecodeTx(rawTx []byte) (*policy.Operation, error) // validates tx: format, chain id, expiration
	SignTx(rawTx []byte, privateKey []byte) ([]byte, string, error)
}

type Service struct {
	log     *logger.Logger
	keys    keyProvider
	signers map[constants.Network]TxSigner
}

func NewService(log *logger.Logger, keys keyProvider, signers map[constants.Network]TxSigner) *Service {
	return &Service{
		log:     log,
		keys:    keys,
		signers: signers,
	}
}

// SignTransaction signs unsigned tx with service key. Tx is sent by the caller.
func (s *Service) SignTransaction(ctx context.Context, params SignTransactionParams) (*SignedTransaction, error) {
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
	err = s.keys.UseSigningKey(ctx, params.Service, params.KeyID, params.Network,
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

// checkPolicy allows operation if tx is from the key and some rule of key role matches it. Returns matched rule index.
func (s *Service) checkPolicy(ctx context.Context, key *keystorage.WalletKey, op *policy.Operation) (int, error) {
	if op.From != "" && op.From != key.Address {
		return 0, ErrInvalidTransaction.WithMessage("tx owner is not the key address")
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
