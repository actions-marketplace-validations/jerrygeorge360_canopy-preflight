package fixture

import (
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
)

var ContractConfig = &PluginConfig{
	SupportedTransactions: []string{"send"},
	TransactionTypeUrls:   []string{"type.googleapis.com/types.MessageSend"},
	CustomStatePrefixes:   [][]byte{{100}},
}

func init() {
	var fds [][]byte
	for _, file := range []protoreflect.FileDescriptor{File_tx_proto, File_broken_proto} {
		fd, _ := proto.Marshal(protodesc.ToFileDescriptorProto(file))
		fds = append(fds, fd)
	}
	ContractConfig.FileDescriptorProtos = fds
}
