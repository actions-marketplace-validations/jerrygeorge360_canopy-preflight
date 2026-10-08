package fixture

type PluginConfig struct {
	SupportedTransactions []string
	TransactionTypeUrls   []string
	CustomStatePrefixes   [][]byte
}

func transactionNames() []string { return []string{"send"} }

var ContractConfig = PluginConfig{
	SupportedTransactions: transactionNames(),
	TransactionTypeUrls:   []string{"type.googleapis.com/demo.MessageSend"},
	CustomStatePrefixes:   [][]byte{{}},
}
