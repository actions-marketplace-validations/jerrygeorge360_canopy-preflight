package fixture

type PluginConfig struct {
	SupportedTransactions []string
	TransactionTypeUrls   []string
	CustomStatePrefixes   [][]byte
}

var ContractConfig = &PluginConfig{}
