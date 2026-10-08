package fixture

var ContractConfig = &PluginConfig{
	SupportedTransactions: []string{"send"},
	TransactionTypeUrls:   []string{"type.googleapis.com/types.MessageSend"},
	CustomStatePrefixes:   [][]byte{{100}},
}
