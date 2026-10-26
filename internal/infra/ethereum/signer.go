package ethereum

import (
	"errors"
	"fmt"
	"math/big"

	"gitlab.tve-development.com/backend/gtoken/wallet-service/internal/policy"
)

type Signer struct {
	chainID *big.Int
}

func NewSigner(chainID *big.Int) *Signer {
	return &Signer{chainID: chainID}
}

func (s *Signer) DecodeTx(rawTx []byte) (*policy.Operation, error) {
	info, err := DecodeTx(rawTx)
	if err != nil {
		return nil, err
	}

	if info.ChainID != nil && info.ChainID.Cmp(s.chainID) != 0 {
		return nil, fmt.Errorf("chain id mismatch: tx %s, want %s", info.ChainID, s.chainID)
	}

	if info.To == "" {
		return nil, errors.New("contract creation is not allowed")
	}

	if len(info.Data) == 0 {
		return &policy.Operation{Op: policy.OpNative, To: info.To, Amount: info.Value}, nil
	}

	if to, amount, ok := DecodeERC20Transfer(info.Data); ok && info.Value.Sign() == 0 {
		return &policy.Operation{Op: policy.OpToken, Contract: info.To, To: to, Amount: amount}, nil
	}

	if len(info.Data) < 4 {
		return nil, errors.New("call data is shorter than method selector")
	}

	op := &policy.Operation{Op: policy.OpContractCall, Contract: info.To, Value: info.Value}
	copy(op.Selector[:], info.Data[:4])

	return op, nil
}

func (s *Signer) SignTx(rawTx []byte, privateKey []byte) ([]byte, string, error) {
	return SignTx(rawTx, privateKey, s.chainID)
}
