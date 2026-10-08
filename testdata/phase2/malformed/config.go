package fixture

type PluginConfig struct {
	SupportedTransactions []string
	TransactionTypeUrls   []string
}

var ContractConfig = PluginConfig{
	SupportedTransactions: []string{"send"},
	TransactionTypeUrls:   []string{"type.googleapis.com/demo.MessageSend"},
}
