package ethereum

import (
	"bytes"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
)

var erc20TransferSelector = []byte{0xa9, 0x05, 0x9c, 0xbb}

type TxInfo struct {
	Type      uint8
	ChainID   *big.Int // nil for legacy tx, chain id is set on signing
	Nonce     uint64
	To        string // "" for contract creation
	Value     *big.Int
	Data      []byte
	Gas       uint64
	GasFeeCap *big.Int // gas price for legacy tx
	GasTipCap *big.Int // gas price for legacy tx
}

func DecodeTx(raw []byte) (*TxInfo, error) {
	tx := new(types.Transaction)
	if err := tx.UnmarshalBinary(raw); err != nil {
		return nil, fmt.Errorf("decode tx: %w", err)
	}

	info := &TxInfo{
		Type:      tx.Type(),
		Nonce:     tx.Nonce(),
		Value:     tx.Value(),
		Data:      tx.Data(),
		Gas:       tx.Gas(),
		GasFeeCap: tx.GasFeeCap(),
		GasTipCap: tx.GasTipCap(),
	}
	if tx.Type() != types.LegacyTxType {
		info.ChainID = tx.ChainId()
	}

	if tx.To() != nil {
		info.To = tx.To().Hex()
	}

	return info, nil
}

func DecodeERC20Transfer(data []byte) (string, *big.Int, bool) {
	if len(data) != 4+32+32 || !bytes.Equal(data[:4], erc20TransferSelector) {
		return "", nil, false
	}

	to := common.BytesToAddress(data[4:36])
	amount := new(big.Int).SetBytes(data[36:68])

	return to.Hex(), amount, true
}

func SignTx(raw []byte, privateKey []byte, chainID *big.Int) ([]byte, string, error) {
	if chainID == nil || chainID.Sign() <= 0 {
		return nil, "", errors.New("invalid chain id")
	}

	tx := new(types.Transaction)
	if err := tx.UnmarshalBinary(raw); err != nil {
		return nil, "", fmt.Errorf("decode tx: %w", err)
	}

	if tx.Type() != types.LegacyTxType && tx.ChainId().Cmp(chainID) != 0 {
		return nil, "", fmt.Errorf("chain id mismatch: tx %s, want %s", tx.ChainId(), chainID)
	}

	keyBytes := make([]byte, hex.DecodedLen(len(privateKey)))
	defer clear(keyBytes)

	if _, err := hex.Decode(keyBytes, privateKey); err != nil {
		return nil, "", fmt.Errorf("decode private key: %w", err)
	}

	key, err := crypto.ToECDSA(keyBytes)
	if err != nil {
		return nil, "", fmt.Errorf("parse private key: %w", err)
	}
	defer clear(key.D.Bits()) //nolint:staticcheck // no stdlib API for secp256k1 keys

	signedTx, err := types.SignTx(tx, types.LatestSignerForChainID(chainID), key)
	if err != nil {
		return nil, "", fmt.Errorf("sign tx: %w", err)
	}

	signed, err := signedTx.MarshalBinary()
	if err != nil {
		return nil, "", fmt.Errorf("encode signed tx: %w", err)
	}

	return signed, signedTx.Hash().Hex(), nil
}
