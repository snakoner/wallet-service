package policy

import (
	"errors"
	"math/big"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	tronlib "github.com/w3-developers/go-tron-lib"

	"gitlab.tve-development.com/backend/gtoken/wallet-service/internal/constants"
)

var (
	token    = common.HexToAddress("0x2222222222222222222222222222222222222222").Hex()
	manager  = common.HexToAddress("0x3333333333333333333333333333333333333333").Hex()
	stranger = common.HexToAddress("0x4444444444444444444444444444444444444444").Hex()
	internal = common.HexToAddress("0x5555555555555555555555555555555555555555").Hex()
)

func selector(signature string) [4]byte {
	var s [4]byte
	copy(s[:], crypto.Keccak256([]byte(signature))[:4])
	return s
}

func mustParse(t *testing.T, raw string) *Policy {
	t.Helper()
	p, err := Parse([]byte(raw), constants.NetworkEthereum)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	return p
}

// internalStub treats internal address as key of gas_station purpose.
func internalStub(calls *int) IsInternal {
	return func(address, purpose string) (bool, error) {
		*calls++
		return address == internal && (purpose == "" || purpose == "gas_station"), nil
	}
}

func TestParseEmpty(t *testing.T) {
	for _, raw := range []string{"", "{}", " {} "} {
		p, err := Parse([]byte(raw), constants.NetworkEthereum)
		if err != nil || p.Version != 1 || len(p.Rules) != 0 {
			t.Fatalf("%q: policy = %+v, err = %v", raw, p, err)
		}
		if string(p.JSON()) != `{"version":1,"rules":[]}` {
			t.Fatalf("%q: json = %s", raw, p.JSON())
		}
		if _, ok, _ := p.Allow(Operation{Op: OpNative, To: stranger}, nil); ok {
			t.Fatal("empty policy must deny")
		}
	}
}

func TestParseNormalizes(t *testing.T) {
	p := mustParse(t, `{"version":1,"rules":[
		{"op":"token","contracts":["`+strings.ToLower(token)+`"],"to":["*"]},
		{"op":"contract_call","contracts":["`+strings.ToLower(manager)+`"],"methods":["transferScaled(address,uint256)"]}
	]}`)

	if p.Rules[0].Contracts[0] != token || p.Rules[1].Contracts[0] != manager || p.Rules[1].Value != "zero" {
		t.Fatalf("rules = %+v", p.Rules)
	}
	if json := string(p.JSON()); !strings.Contains(json, token) || !strings.Contains(json, `"value":"zero"`) {
		t.Fatalf("json = %s", json)
	}

	_, address, err := tronlib.GenerateAddress()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Parse([]byte(`{"version":1,"rules":[{"op":"native","to":["`+address+`"]}]}`), constants.NetworkTron); err != nil {
		t.Fatalf("tron address: err = %v", err)
	}
}

func TestParseErrors(t *testing.T) {
	_, tronAddress, err := tronlib.GenerateAddress()
	if err != nil {
		t.Fatal(err)
	}

	cases := map[string]string{
		"not json":                `{`,
		"unknown field":           `{"version":1,"rules":[{"op":"native","to":["*"],"limit":1}]}`,
		"no version":              `{"rules":[]}`,
		"unknown op":              `{"version":1,"rules":[{"op":"deploy"}]}`,
		"native without to":       `{"version":1,"rules":[{"op":"native"}]}`,
		"native with contracts":   `{"version":1,"rules":[{"op":"native","to":["*"],"contracts":["` + token + `"]}]}`,
		"token without contracts": `{"version":1,"rules":[{"op":"token","to":["*"]}]}`,
		"token with methods":      `{"version":1,"rules":[{"op":"token","contracts":["` + token + `"],"to":["*"],"methods":["a()"]}]}`,
		"call without methods":    `{"version":1,"rules":[{"op":"contract_call","contracts":["` + manager + `"]}]}`,
		"call with to":            `{"version":1,"rules":[{"op":"contract_call","contracts":["` + manager + `"],"methods":["a()"],"to":["*"]}]}`,
		"call with max_amount":    `{"version":1,"rules":[{"op":"contract_call","contracts":["` + manager + `"],"methods":["a()"],"max_amount":"1"}]}`,
		"bad value":               `{"version":1,"rules":[{"op":"contract_call","contracts":["` + manager + `"],"methods":["a()"],"value":"some"}]}`,
		"method with spaces":      `{"version":1,"rules":[{"op":"contract_call","contracts":["` + manager + `"],"methods":["transfer(address, uint256)"]}]}`,
		"method with names":       `{"version":1,"rules":[{"op":"contract_call","contracts":["` + manager + `"],"methods":["transfer(address to)"]}]}`,
		"method unbalanced":       `{"version":1,"rules":[{"op":"contract_call","contracts":["` + manager + `"],"methods":["f((address)"]}]}`,
		"bad contract":            `{"version":1,"rules":[{"op":"token","contracts":["0x12"],"to":["*"]}]}`,
		"tron address in eth":     `{"version":1,"rules":[{"op":"native","to":["` + tronAddress + `"]}]}`,
		"empty internal purpose":  `{"version":1,"rules":[{"op":"native","to":["internal:"]}]}`,
		"negative max_amount":     `{"version":1,"rules":[{"op":"native","to":["*"],"max_amount":"-1"}]}`,
		"fractional max_amount":   `{"version":1,"rules":[{"op":"native","to":["*"],"max_amount":"1.5"}]}`,
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := Parse([]byte(raw), constants.NetworkEthereum); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestAllow(t *testing.T) {
	p := mustParse(t, `{"version":1,"rules":[
		{"op":"native","to":["internal:gas_station"],"max_amount":"100"},
		{"op":"token","contracts":["`+token+`"],"to":["`+stranger+`"]},
		{"op":"contract_call","contracts":["`+manager+`"],"methods":["transferTokensWithNative(address,uint256,uint256)"]},
		{"op":"contract_call","contracts":["`+manager+`"],"methods":["withdraw()"],"value":"any"}
	]}`)

	call := selector("transferTokensWithNative(address,uint256,uint256)")
	cases := []struct {
		name string
		op   Operation
		rule int
		ok   bool
	}{
		{"native to internal gas station", Operation{Op: OpNative, To: internal, Amount: big.NewInt(100)}, 0, true},
		{"native over max_amount", Operation{Op: OpNative, To: internal, Amount: big.NewInt(101)}, 0, false},
		{"native to stranger", Operation{Op: OpNative, To: stranger, Amount: big.NewInt(1)}, 0, false},
		{"token to allowed address", Operation{Op: OpToken, Contract: token, To: stranger, Amount: big.NewInt(5)}, 1, true},
		{"token of other contract", Operation{Op: OpToken, Contract: manager, To: stranger, Amount: big.NewInt(5)}, 0, false},
		{"token to internal", Operation{Op: OpToken, Contract: token, To: internal, Amount: big.NewInt(5)}, 0, false},
		{"allowed method", Operation{Op: OpContractCall, Contract: manager, Selector: call, Value: big.NewInt(0)}, 2, true},
		{"allowed method with value", Operation{Op: OpContractCall, Contract: manager, Selector: call, Value: big.NewInt(1)}, 0, false},
		{"other method", Operation{Op: OpContractCall, Contract: manager, Selector: selector("approve(address,uint256)")}, 0, false},
		{"method on other contract", Operation{Op: OpContractCall, Contract: token, Selector: call}, 0, false},
		{"value any", Operation{Op: OpContractCall, Contract: manager, Selector: selector("withdraw()"), Value: big.NewInt(7)}, 3, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var calls int
			rule, ok, err := p.Allow(tc.op, internalStub(&calls))
			if err != nil || ok != tc.ok || (ok && rule != tc.rule) {
				t.Fatalf("Allow() = %d, %v, %v; want %d, %v", rule, ok, err, tc.rule, tc.ok)
			}
		})
	}
}

func TestAllowInternalLookup(t *testing.T) {
	p := mustParse(t, `{"version":1,"rules":[{"op":"native","to":["`+stranger+`","internal"]}]}`)

	var calls int
	if _, ok, _ := p.Allow(Operation{Op: OpNative, To: stranger, Amount: big.NewInt(1)}, internalStub(&calls)); !ok || calls != 0 {
		t.Fatalf("plain address: ok = %v, internal lookups = %d", ok, calls)
	}
	if _, ok, _ := p.Allow(Operation{Op: OpNative, To: internal, Amount: big.NewInt(1)}, internalStub(&calls)); !ok || calls != 1 {
		t.Fatalf("internal: ok = %v, internal lookups = %d", ok, calls)
	}

	lookupErr := errors.New("db down")
	_, ok, err := p.Allow(Operation{Op: OpNative, To: internal}, func(string, string) (bool, error) { return false, lookupErr })
	if ok || !errors.Is(err, lookupErr) {
		t.Fatalf("lookup error: ok = %v, err = %v", ok, err)
	}
}
