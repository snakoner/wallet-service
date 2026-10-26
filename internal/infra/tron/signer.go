package tron

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum/crypto"
	tronlib "github.com/w3-developers/go-tron-lib"
	"google.golang.org/protobuf/encoding/protowire"

	"gitlab.tve-development.com/backend/gtoken/wallet-service/internal/policy"
)

// protocol.Transaction.raw field numbers
const (
	rawExpirationField protowire.Number = 8
	rawContractField   protowire.Number = 11
)

// protocol.Transaction field numbers
const (
	txRawDataField   protowire.Number = 1
	txSignatureField protowire.Number = 2
)

// protocol.Transaction.Contract: type, parameter (google.protobuf.Any: type_url, value)
const (
	contractTypeField      protowire.Number = 1
	contractParameterField protowire.Number = 2
	anyValueField          protowire.Number = 2
)

// protocol.Transaction.Contract.ContractType
const (
	transferContractType          = 1
	triggerSmartContractType      = 31
	addressPrefix            byte = 0x41
)

// TransferContract and TriggerSmartContract field numbers
const (
	ownerAddressField   protowire.Number = 1
	toAddressField      protowire.Number = 2 // TransferContract.to_address, TriggerSmartContract.contract_address
	amountField         protowire.Number = 3 // TransferContract.amount, TriggerSmartContract.call_value
	dataField           protowire.Number = 4
	callTokenValueField protowire.Number = 5
)

var trc20TransferSelector = []byte{0xa9, 0x05, 0x9c, 0xbb}

type Signer struct{}

func NewSigner() *Signer {
	return &Signer{}
}

// Only TransferContract and TriggerSmartContract: other types can change account permissions.
func (s *Signer) DecodeTx(rawTx []byte) (*policy.Operation, error) {
	raw, err := parseMessage(rawTx)
	if err != nil {
		return nil, fmt.Errorf("decode tx: %w", err)
	}

	if len(raw[rawContractField]) != 1 {
		return nil, fmt.Errorf("tx must have one contract, got %d", len(raw[rawContractField]))
	}

	if int64(varintField(raw, rawExpirationField)) <= time.Now().UnixMilli() {
		return nil, errors.New("tx expired")
	}

	contract, err := parseMessage(raw[rawContractField][0].bytes)
	if err != nil {
		return nil, fmt.Errorf("decode contract: %w", err)
	}

	parameter, err := parseMessage(bytesField(contract, contractParameterField))
	if err != nil {
		return nil, fmt.Errorf("decode contract parameter: %w", err)
	}

	msg, err := parseMessage(bytesField(parameter, anyValueField))
	if err != nil {
		return nil, fmt.Errorf("decode contract value: %w", err)
	}

	owner, err := addressField(msg, ownerAddressField)
	if err != nil {
		return nil, fmt.Errorf("owner_address: %w", err)
	}

	to, err := addressField(msg, toAddressField)
	if err != nil {
		return nil, fmt.Errorf("to address: %w", err)
	}

	amount := new(big.Int).SetUint64(varintField(msg, amountField))

	switch contractType := varintField(contract, contractTypeField); contractType {
	case transferContractType:
		return &policy.Operation{Op: policy.OpNative, From: owner, To: to, Amount: amount}, nil
	case triggerSmartContractType:
		if varintField(msg, callTokenValueField) != 0 {
			return nil, errors.New("trc10 call_token_value is not supported")
		}

		data := bytesField(msg, dataField)
		if len(data) < 4 {
			return nil, errors.New("call data is shorter than method selector")
		}

		if len(data) == 4+32+32 && bytes.Equal(data[:4], trc20TransferSelector) && amount.Sign() == 0 {
			recipient, err := toBase58(append([]byte{addressPrefix}, data[4+12:36]...))
			if err != nil {
				return nil, fmt.Errorf("transfer recipient: %w", err)
			}

			return &policy.Operation{
				Op:       policy.OpToken,
				From:     owner,
				Contract: to,
				To:       recipient,
				Amount:   new(big.Int).SetBytes(data[36:68]),
			}, nil
		}

		op := &policy.Operation{Op: policy.OpContractCall, From: owner, Contract: to, Value: amount}
		copy(op.Selector[:], data[:4])

		return op, nil
	default:
		return nil, fmt.Errorf("unsupported contract type %d", contractType)
	}
}

type protoField struct {
	varint uint64
	bytes  []byte
}

func parseMessage(b []byte) (map[protowire.Number][]protoField, error) {
	fields := map[protowire.Number][]protoField{}
	for len(b) > 0 {
		num, typ, n := protowire.ConsumeTag(b)
		if n < 0 {
			return nil, protowire.ParseError(n)
		}
		b = b[n:]

		var field protoField
		switch typ {
		case protowire.VarintType:
			field.varint, n = protowire.ConsumeVarint(b)
		case protowire.BytesType:
			field.bytes, n = protowire.ConsumeBytes(b)
		default:
			n = protowire.ConsumeFieldValue(num, typ, b)
		}

		if n < 0 {
			return nil, protowire.ParseError(n)
		}
		b = b[n:]

		if typ == protowire.VarintType || typ == protowire.BytesType {
			fields[num] = append(fields[num], field)
		}
	}

	return fields, nil
}

func varintField(fields map[protowire.Number][]protoField, num protowire.Number) uint64 {
	if values := fields[num]; len(values) > 0 {
		return values[len(values)-1].varint
	}

	return 0
}

func bytesField(fields map[protowire.Number][]protoField, num protowire.Number) []byte {
	if values := fields[num]; len(values) > 0 {
		return values[len(values)-1].bytes
	}

	return nil
}

func addressField(fields map[protowire.Number][]protoField, num protowire.Number) (string, error) {
	return toBase58(bytesField(fields, num))
}

func toBase58(address []byte) (string, error) {
	if len(address) != 21 || address[0] != addressPrefix {
		return "", errors.New("invalid tron address")
	}

	return tronlib.TronHexToBase58(hex.EncodeToString(address))
}

func (s *Signer) SignTx(rawTx []byte, privateKey []byte) ([]byte, string, error) {
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

	hash := sha256.Sum256(rawTx)
	signature, err := crypto.Sign(hash[:], key)
	if err != nil {
		return nil, "", fmt.Errorf("sign tx: %w", err)
	}

	var signed []byte
	signed = protowire.AppendTag(signed, txRawDataField, protowire.BytesType)
	signed = protowire.AppendBytes(signed, rawTx)
	signed = protowire.AppendTag(signed, txSignatureField, protowire.BytesType)
	signed = protowire.AppendBytes(signed, signature)

	return signed, hex.EncodeToString(hash[:]), nil
}
