package fixture

type PluginConfig struct {
	FileDescriptorProtos [][]byte
}

var ContractConfig = &PluginConfig{}

func init() {
	ContractConfig.FileDescriptorProtos = buildDescriptors()
}

func buildDescriptors() [][]byte { return nil }
