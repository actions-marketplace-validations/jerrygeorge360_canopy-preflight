package fixture

var pluginPrefix = []byte{100}

var ContractConfig = &PluginConfig{
	SupportedTransactions: nil,
	TransactionTypeUrls:   nil,
	CustomStatePrefixes:   [][]byte{pluginPrefix},
}
