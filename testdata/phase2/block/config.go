package fixture

var ContractConfig = PluginConfig{
	SupportedTransactions: []string{"send", "reward"},
	TransactionTypeUrls:   []string{"type.googleapis.com/demo.MessageSend"},
	CustomStatePrefixes:   [][]byte{{1}, {7}, {15}},
}
