package policy

import "math/big"

type Op string

const (
	OpNative       Op = "native"        // native coin transfer
	OpToken        Op = "token"         // transfer(address,uint256) of ERC20 / TRC20
	OpContractCall Op = "contract_call" // any other contract call
)

// Operation is what tx does, decoded by network signer. Addresses are normalized (see NormalizeAddress).
type Operation struct {
	Op       Op
	From     string   // tron: owner_address; "" for ethereum, sender is the signing key
	To       string   // native, token: recipient
	Contract string   // token: token contract, contract_call: called contract
	Selector [4]byte  // contract_call
	Amount   *big.Int // native, token
	Value    *big.Int // contract_call: attached native coin
}
