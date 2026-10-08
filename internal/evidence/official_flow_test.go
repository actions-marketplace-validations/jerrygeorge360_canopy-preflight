package evidence

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCollectAcceptsExactOfficialPointerFlow(t *testing.T) {
	got, err := Collect(officialPointerFixture("official-pointer-valid"))
	if err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	if len(got.Configs) != 1 || !got.Configs[0].CanopyType {
		t.Fatalf("official config provenance = %#v", got.Configs)
	}
	if got.Configs[0].Mutated {
		t.Fatalf("exact official flow was not accepted as immutable: %#v", got.Configs[0])
	}
	if !got.SchemaSelection.Attempted || !got.SchemaSelection.Proven || len(got.SchemaSelection.Issues) != 0 {
		t.Fatalf("descriptor selection = %#v", got.SchemaSelection)
	}
	if len(got.SelectedSchemas["types.MessageSend"]) != 1 {
		t.Fatalf("selected MessageSend descriptors = %#v", got.SelectedSchemas["types.MessageSend"])
	}
}

func TestCollectRejectsOfficialPointerFlowDetours(t *testing.T) {
	tests := []struct {
		name string
		file string
		old  string
		new  string
		all  bool
	}{
		{
			name: "extra ContractConfig alias",
			file: "flow.go",
			old:  "if err := p.Handshake(); err != nil {",
			new:  "alias := ContractConfig\n\t_ = alias\n\tif err := p.Handshake(); err != nil {",
		},
		{
			name: "ContractConfig Reset call",
			file: "flow.go",
			old:  "if err := p.Handshake(); err != nil {",
			new:  "ContractConfig.Reset()\n\tif err := p.Handshake(); err != nil {",
		},
		{
			name: "Plugin carrier callback escape",
			file: "flow.go",
			old:  "if err := p.Handshake(); err != nil {",
			new:  "externalCallback(p)\n\tif err := p.Handshake(); err != nil {",
		},
		{
			name: "Plugin carrier alias",
			file: "flow.go",
			old:  "if err := p.Handshake(); err != nil {",
			new:  "alias := p\n\t_ = alias\n\tif err := p.Handshake(); err != nil {",
		},
		{
			name: "unexpected carrier method",
			file: "flow.go",
			old:  "if err := p.Handshake(); err != nil {",
			new:  "p.Mutate()\n\tif err := p.Handshake(); err != nil {",
		},
		{
			name: "pluginConfig reassignment",
			file: "flow.go",
			old:  "if err := p.Handshake(); err != nil {",
			new:  "p.pluginConfig = ContractConfig\n\tif err := p.Handshake(); err != nil {",
		},
		{
			name: "checked field mutation",
			file: "flow.go",
			old:  "if err := p.Handshake(); err != nil {",
			new:  "ContractConfig.SupportedTransactions = nil\n\tif err := p.Handshake(); err != nil {",
		},
		{
			name: "wrong Plugin field name",
			file: "flow.go",
			old:  "pluginConfig",
			new:  "config",
			all:  true,
		},
		{
			name: "wrong Plugin field type",
			file: "flow.go",
			old:  "pluginConfig    *PluginConfig",
			new:  "pluginConfig    any",
		},
		{
			name: "wrong generated wrapper type",
			file: "types.pb.go",
			old:  "Config *PluginConfig `protobuf:\"bytes,2,opt,name=config,proto3,oneof\"`",
			new:  "Config any `protobuf:\"bytes,2,opt,name=config,proto3,oneof\"`",
		},
		{
			name: "wrong generated wrapper tag",
			file: "types.pb.go",
			old:  "protobuf:\"bytes,2,opt,name=config,proto3,oneof\"",
			new:  "protobuf:\"bytes,3,opt,name=config,proto3,oneof\"",
		},
		{
			name: "wrapper detour",
			file: "flow.go",
			old:  "response, err := p.sendToPluginSync(&Contract{}, &PluginToFSM_Config{Config: p.pluginConfig})",
			new:  "wrapper := &PluginToFSM_Config{Config: p.pluginConfig}\n\tresponse, err := p.sendToPluginSync(&Contract{}, wrapper)",
		},
		{
			name: "marshalled bytes are not sent",
			file: "flow.go",
			old:  "bz, err := Marshal(ptr)\n\tif err != nil {\n\t\treturn err\n\t}\n\treturn p.sendLengthPrefixed(bz)",
			new:  "_, _ = Marshal(ptr)\n\treturn nil",
		},
		{
			name: "marshal returns unrelated bytes",
			file: "flow.go",
			old:  "return protoBytes, nil",
			new:  "return []byte{1}, nil",
		},
		{
			name: "socket send hidden in dead branch",
			file: "flow.go",
			old:  "return p.sendLengthPrefixed(bz)",
			new:  "if false {\n\t\treturn p.sendLengthPrefixed(bz)\n\t}\n\treturn nil",
		},
		{
			name: "marshal result return hidden in dead branch",
			file: "flow.go",
			old:  "return protoBytes, nil",
			new:  "if false {\n\t\treturn protoBytes, nil\n\t}\n\treturn []byte{1}, nil",
		},
		{
			name: "handshake send hidden in dead branch",
			file: "flow.go",
			old:  "response, err := p.sendToPluginSync(&Contract{}, &PluginToFSM_Config{Config: p.pluginConfig})",
			new:  "if false {\n\t\t_, _ = p.sendToPluginSync(&Contract{}, &PluginToFSM_Config{Config: p.pluginConfig})\n\t}\n\tvar response isFSMToPlugin_Payload\n\tvar err *PluginError",
		},
		{
			name: "early return before socket send",
			file: "flow.go",
			old:  "bz, err := Marshal(ptr)",
			new:  "return nil\n\tbz, err := Marshal(ptr)",
		},
		{
			name: "early return before marshal",
			file: "flow.go",
			old:  "protoBytes, err := marshaller.Marshal(message.(proto.Message))",
			new:  "return nil, nil\n\tprotoBytes, err := marshaller.Marshal(message.(proto.Message))",
		},
		{
			name: "early return before handshake handoff",
			file: "flow.go",
			old:  "response, err := p.sendToPluginSync(&Contract{}, &PluginToFSM_Config{Config: p.pluginConfig})",
			new:  "return nil\n\tresponse, err := p.sendToPluginSync(&Contract{}, &PluginToFSM_Config{Config: p.pluginConfig})",
		},
		{
			name: "early return before synchronous forwarding",
			file: "flow.go",
			old:  "ch, requestId, err := p.sendToPluginAsync(c, request)",
			new:  "return nil, nil\n\tch, requestId, err := p.sendToPluginAsync(c, request)",
		},
		{
			name: "early return before asynchronous forwarding",
			file: "flow.go",
			old:  "err = p.sendProtoMsg(&PluginToFSM{Id: requestId, Payload: request})",
			new:  "return\n\terr = p.sendProtoMsg(&PluginToFSM{Id: requestId, Payload: request})",
		},
		{
			name: "panic before handshake handoff",
			file: "flow.go",
			old:  "response, err := p.sendToPluginSync(&Contract{}, &PluginToFSM_Config{Config: p.pluginConfig})",
			new:  "panic(\"stop\")\n\tresponse, err := p.sendToPluginSync(&Contract{}, &PluginToFSM_Config{Config: p.pluginConfig})",
		},
		{
			name: "unexpected call before handshake handoff",
			file: "flow.go",
			old:  "response, err := p.sendToPluginSync(&Contract{}, &PluginToFSM_Config{Config: p.pluginConfig})",
			new:  "abort()\n\tresponse, err := p.sendToPluginSync(&Contract{}, &PluginToFSM_Config{Config: p.pluginConfig})",
		},
		{
			name: "panicking expression before handshake handoff",
			file: "flow.go",
			old:  "response, err := p.sendToPluginSync(&Contract{}, &PluginToFSM_Config{Config: p.pluginConfig})",
			new:  "_ = []int{}[0]\n\tresponse, err := p.sendToPluginSync(&Contract{}, &PluginToFSM_Config{Config: p.pluginConfig})",
		},
		{
			name: "marshaller reassignment",
			file: "flow.go",
			old:  "var marshaller = proto.MarshalOptions{Deterministic: true}",
			new:  "var marshaller = proto.MarshalOptions{Deterministic: true}\n\nfunc init() { marshaller = proto.MarshalOptions{} }",
		},
		{
			name: "marshal returns bytes with non-nil error",
			file: "flow.go",
			old:  "return protoBytes, nil",
			new:  "return protoBytes, &PluginError{}",
		},
		{
			name: "infinite loop before handshake handoff",
			file: "flow.go",
			old:  "response, err := p.sendToPluginSync(&Contract{}, &PluginToFSM_Config{Config: p.pluginConfig})",
			new:  "for {}\n\tresponse, err := p.sendToPluginSync(&Contract{}, &PluginToFSM_Config{Config: p.pluginConfig})",
		},
		{
			name: "blocking receive before handshake handoff",
			file: "flow.go",
			old:  "response, err := p.sendToPluginSync(&Contract{}, &PluginToFSM_Config{Config: p.pluginConfig})",
			new:  "<-make(chan struct{})\n\tresponse, err := p.sendToPluginSync(&Contract{}, &PluginToFSM_Config{Config: p.pluginConfig})",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := copyOfficialPointerFixture(t, "official-pointer-valid")
			replaceOfficialFixtureText(t, root, test.file, test.old, test.new, test.all)
			got, err := Collect(root)
			if err != nil {
				t.Fatalf("Collect() error = %v", err)
			}
			if len(got.Configs) != 1 || !got.Configs[0].CanopyType {
				t.Fatalf("adversarial case lost baseline config provenance: %#v", got.Configs)
			}
			if !got.Configs[0].Mutated {
				t.Fatalf("unsupported official-flow variant was trusted: %#v", got.Configs[0])
			}
		})
	}
}

func officialPointerFixture(name string) string {
	return filepath.Join("..", "..", "testdata", "phase2", name)
}

func copyOfficialPointerFixture(t *testing.T, name string) string {
	t.Helper()
	source := officialPointerFixture(name)
	target := t.TempDir()
	entries, err := os.ReadDir(source)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(source, entry.Name()))
		if err != nil {
			t.Fatalf("read fixture file: %v", err)
		}
		if err := os.WriteFile(filepath.Join(target, entry.Name()), data, 0o600); err != nil {
			t.Fatalf("copy fixture file: %v", err)
		}
	}
	return target
}

func replaceOfficialFixtureText(t *testing.T, root, name, old, replacement string, all bool) {
	t.Helper()
	path := filepath.Join(root, name)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	if !strings.Contains(string(data), old) {
		t.Fatalf("%s does not contain mutation target %q", name, old)
	}
	count := 1
	if all {
		count = -1
	}
	updated := strings.Replace(string(data), old, replacement, count)
	if err := os.WriteFile(path, []byte(updated), 0o600); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}
