package policy

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"regexp"
	"slices"
	"strings"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	tronlib "github.com/w3-developers/go-tron-lib"

	"gitlab.tve-development.com/backend/gtoken/wallet-service/internal/constants"
)

const (
	version = 1

	toAny            = "*"
	toInternal       = "internal"
	toInternalPrefix = "internal:"

	valueZero = "zero"
	valueAny  = "any"
)

var methodSignature = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*\([^\s]*\)$`)

type Rule struct {
	Op        Op       `json:"op"`
	Contracts []string `json:"contracts,omitempty"`
	Methods   []string `json:"methods,omitempty"`
	To        []string `json:"to,omitempty"`
	MaxAmount *string  `json:"max_amount,omitempty"`
	Value     string   `json:"value,omitempty"`

	selectors [][4]byte
	maxAmount *big.Int
}

type Policy struct {
	Version int    `json:"version"`
	Rules   []Rule `json:"rules"`
}

// IsInternal reports whether address is a key of the same service and network; purpose "" - any purpose.
type IsInternal func(address, purpose string) (bool, error)

// Parse validates policy of role in network and normalizes addresses. Empty or {} policy denies everything.
func Parse(raw json.RawMessage, network constants.Network) (*Policy, error) {
	if len(bytes.TrimSpace(raw)) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("{}")) {
		return &Policy{Version: version, Rules: []Rule{}}, nil
	}

	p := &Policy{}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(p); err != nil {
		return nil, fmt.Errorf("decode policy: %w", err)
	}

	if p.Version != version {
		return nil, fmt.Errorf("unsupported policy version %d", p.Version)
	}

	if p.Rules == nil {
		p.Rules = []Rule{}
	}

	for i := range p.Rules {
		if err := p.Rules[i].compile(network); err != nil {
			return nil, fmt.Errorf("rule %d: %w", i, err)
		}
	}

	return p, nil
}

// JSON returns normalized policy for storage.
func (p *Policy) JSON() json.RawMessage {
	data, _ := json.Marshal(p)
	return data
}

func (r *Rule) compile(network constants.Network) error {
	switch r.Op {
	case OpNative:
		if len(r.Contracts) > 0 || len(r.Methods) > 0 || r.Value != "" {
			return errors.New("native rule supports only to and max_amount")
		}
	case OpToken:
		if len(r.Contracts) == 0 || len(r.Methods) > 0 || r.Value != "" {
			return errors.New("token rule requires contracts and supports only to and max_amount")
		}
	case OpContractCall:
		if len(r.Contracts) == 0 || len(r.Methods) == 0 || len(r.To) > 0 || r.MaxAmount != nil {
			return errors.New("contract_call rule requires contracts and methods and supports only value")
		}

		if r.Value == "" {
			r.Value = valueZero
		}

		if r.Value != valueZero && r.Value != valueAny {
			return fmt.Errorf("value must be %q or %q", valueZero, valueAny)
		}
	default:
		return fmt.Errorf("unsupported op %q", r.Op)
	}

	if r.Op != OpContractCall && len(r.To) == 0 {
		return errors.New("to is required")
	}

	for i, contract := range r.Contracts {
		address, err := NormalizeAddress(network, contract)
		if err != nil {
			return fmt.Errorf("contract %q: %w", contract, err)
		}
		r.Contracts[i] = address
	}

	for i, to := range r.To {
		switch {
		case to == toAny || to == toInternal:
		case strings.HasPrefix(to, toInternalPrefix):
			if strings.TrimPrefix(to, toInternalPrefix) == "" {
				return errors.New("internal purpose is empty")
			}
		default:
			address, err := NormalizeAddress(network, to)
			if err != nil {
				return fmt.Errorf("to %q: %w", to, err)
			}
			r.To[i] = address
		}
	}

	for _, method := range r.Methods {
		if !methodSignature.MatchString(method) || strings.Count(method, "(") != strings.Count(method, ")") {
			return fmt.Errorf("method %q must be canonical signature like transfer(address,uint256)", method)
		}

		var selector [4]byte
		copy(selector[:], crypto.Keccak256([]byte(method))[:4])
		r.selectors = append(r.selectors, selector)
	}

	if r.MaxAmount != nil {
		amount, ok := new(big.Int).SetString(*r.MaxAmount, 10)
		if !ok || amount.Sign() < 0 || strings.HasPrefix(*r.MaxAmount, "+") {
			return fmt.Errorf("max_amount %q must be non-negative integer", *r.MaxAmount)
		}
		r.maxAmount = amount
	}

	return nil
}

// NormalizeAddress validates address for network: checksum hex for ethereum and arbitrum, base58 for tron.
func NormalizeAddress(network constants.Network, address string) (string, error) {
	switch network {
	case constants.NetworkEthereum, constants.NetworkArbitrum:
		if !common.IsHexAddress(address) {
			return "", errors.New("invalid ethereum address")
		}

		return common.HexToAddress(address).Hex(), nil
	case constants.NetworkTron:
		addressHex, err := tronlib.TronBase58ToHex(address)
		if err != nil {
			return "", fmt.Errorf("invalid tron address: %w", err)
		}

		return tronlib.TronHexToBase58(addressHex)
	default:
		return "", errors.New("unsupported network")
	}
}

// Allow returns index of first rule matching operation. Policy without matching rule denies operation.
func (p *Policy) Allow(op Operation, isInternal IsInternal) (int, bool, error) {
	for i := range p.Rules {
		ok, err := p.Rules[i].match(op, isInternal)
		if err != nil {
			return 0, false, err
		}

		if ok {
			return i, true, nil
		}
	}

	return 0, false, nil
}

func (r *Rule) match(op Operation, isInternal IsInternal) (bool, error) {
	if r.Op != op.Op {
		return false, nil
	}

	if len(r.Contracts) > 0 && !slices.Contains(r.Contracts, op.Contract) {
		return false, nil
	}

	if op.Op == OpContractCall {
		if !slices.Contains(r.selectors, op.Selector) {
			return false, nil
		}

		return r.Value == valueAny || op.Value == nil || op.Value.Sign() == 0, nil
	}

	if r.maxAmount != nil && (op.Amount == nil || op.Amount.Cmp(r.maxAmount) > 0) {
		return false, nil
	}

	return r.matchTo(op.To, isInternal)
}

// matchTo checks plain values first and asks storage about internal addresses only if needed.
func (r *Rule) matchTo(to string, isInternal IsInternal) (bool, error) {
	for _, allowed := range r.To {
		if allowed == toAny || allowed == to {
			return true, nil
		}
	}

	for _, allowed := range r.To {
		purpose, ok := strings.CutPrefix(allowed, toInternalPrefix)
		if allowed == toInternal {
			purpose, ok = "", true
		}

		if !ok {
			continue
		}

		internal, err := isInternal(to, purpose)
		if err != nil {
			return false, err
		}

		if internal {
			return true, nil
		}
	}

	return false, nil
}
