package constants

type Network int8

const (
	NetworkEthereum Network = 1
	NetworkTron     Network = 2
	NetworkArbitrum Network = 3
)

var NetworkStrings = map[Network]string{
	NetworkEthereum: "ethereum",
	NetworkTron:     "tron",
	NetworkArbitrum: "arbitrum",
}

var Networks = make(map[string]Network, len(NetworkStrings))

func init() {
	for network, name := range NetworkStrings {
		Networks[name] = network
	}
}

func (n Network) String() string {
	return NetworkStrings[n]
}

func ParseNetwork(s string) (Network, bool) {
	if network, ok := Networks[s]; ok {
		return network, true
	}

	return NetworkEthereum, false
}
