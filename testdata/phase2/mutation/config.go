package fixture

var ContractConfig = PluginConfig{
	SupportedTransactions: []string{"send"},
	TransactionTypeUrls:   []string{"type.googleapis.com/demo.MessageSend"},
	CustomStatePrefixes:   [][]byte{{100}},
}

func init() {
	ContractConfig.CustomStatePrefixes = append(ContractConfig.CustomStatePrefixes, []byte{7})
}
