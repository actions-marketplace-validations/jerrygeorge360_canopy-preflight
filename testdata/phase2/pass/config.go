package fixture

var ContractConfig = &PluginConfig{
	SupportedTransactions: nil,
	TransactionTypeUrls:   nil,
	CustomStatePrefixes:   [][]byte{{0}, {16}, {100}, {255}, {1, 2}},
}
