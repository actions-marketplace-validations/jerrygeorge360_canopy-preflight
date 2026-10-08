package evidence

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

func TestCollectOfficialStylePointerConfigAndLiteralPrefixes(t *testing.T) {
	root := phase2Fixture("pass")
	before, err := os.ReadFile(filepath.Join(root, "config.go"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	got, err := Collect(root)
	if err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	after, err := os.ReadFile(filepath.Join(root, "config.go"))
	if err != nil {
		t.Fatalf("reread fixture: %v", err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("Collect mutated the inspected project")
	}
	if len(got.Configs) != 1 || !got.Configs[0].CanopyType || got.DescriptorCount != 1 || len(got.DescriptorIssues) != 0 {
		t.Fatalf("unexpected evidence: %#v", got)
	}
	if _, ok := got.Messages["types.PluginConfig"]; !ok {
		t.Fatal("types.PluginConfig descriptor was not collected")
	}
	wantPrefixes := [][]byte{{0}, {16}, {100}, {255}, {1, 2}}
	var prefixes [][]byte
	for _, prefix := range got.Configs[0].CustomStatePrefixes.Values {
		if prefix.Dynamic {
			t.Fatalf("valid prefix classified dynamic: %#v", prefix)
		}
		prefixes = append(prefixes, prefix.Bytes)
	}
	if !reflect.DeepEqual(prefixes, wantPrefixes) {
		t.Fatalf("prefixes = %#v, want %#v", prefixes, wantPrefixes)
	}
}

func TestCollectOneHopPrefixIsDynamic(t *testing.T) {
	got, err := Collect(phase2Fixture("one-hop"))
	if err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	if len(got.Configs) != 1 || !got.Configs[0].CanopyType {
		t.Fatalf("unexpected config evidence: %#v", got.Configs)
	}
	prefixes := got.Configs[0].CustomStatePrefixes.Values
	if len(prefixes) != 1 || !prefixes[0].Dynamic {
		t.Fatalf("one-hop mutable prefix must require review: %#v", prefixes)
	}
}

func TestCollectDetectsMutationAndMultipleConfigs(t *testing.T) {
	mutated, err := Collect(phase2Fixture("mutation"))
	if err != nil {
		t.Fatalf("Collect(mutation) error = %v", err)
	}
	if len(mutated.Configs) != 1 || !mutated.Configs[0].Mutated {
		t.Fatalf("mutation not detected: %#v", mutated.Configs)
	}
	multiple, err := Collect(phase2Fixture("multiple"))
	if err != nil {
		t.Fatalf("Collect(multiple) error = %v", err)
	}
	if len(multiple.Configs) != 2 {
		t.Fatalf("config count = %d, want 2", len(multiple.Configs))
	}
	if multiple.Configs[0].Loc.Path != "first/config.go" || multiple.Configs[1].Loc.Path != "second/config.go" {
		t.Fatalf("configs are not deterministically ordered: %#v", multiple.Configs)
	}
}

func TestCollectDoesNotTrustUnrelatedLocalPluginConfigType(t *testing.T) {
	root := t.TempDir()
	writeGo(t, root, "config.go", "package fixture\ntype PluginConfig struct { SupportedTransactions []string; TransactionTypeUrls []string }\nvar ContractConfig = PluginConfig{SupportedTransactions: []string{\"send\", \"reward\"}, TransactionTypeUrls: []string{\"MessageSend\"}}\n")
	got, err := Collect(root)
	if err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	if len(got.Configs) != 1 || got.Configs[0].CanopyType {
		t.Fatalf("local lookalike type was trusted: %#v", got.Configs)
	}
}

func TestCollectDoesNotTrustNameOnlyPluginConfigDescriptor(t *testing.T) {
	root := t.TempDir()
	writeGo(t, root, "config.go", "package fixture\nvar ContractConfig = PluginConfig{}\n")
	writeGo(t, root, "types.pb.go", `package fixture
type PluginConfig struct {
	Name string; Id uint64; Version uint64
	SupportedTransactions []string; FileDescriptorProtos [][]byte
	TransactionTypeUrls []string; EventTypeUrls []string; CustomStatePrefixes [][]byte
}
var file_types_rawDesc = "\x12\x05types\x22\x0e\x0a\x0cPluginConfig"
`)
	got, err := Collect(root)
	if err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	if len(got.Configs) != 1 || got.Configs[0].CanopyType {
		t.Fatalf("name-only descriptor was trusted: %#v", got.Configs)
	}
}

func TestCollectRequiresGeneratedTypeShapeAndSameGoPackage(t *testing.T) {
	generated, err := os.ReadFile(filepath.Join(phase2Fixture("pass"), "demo.pb.go"))
	if err != nil {
		t.Fatalf("read generated fixture: %v", err)
	}
	tests := []struct {
		name   string
		source string
	}{
		{name: "wrong field tag", source: strings.Replace(string(generated), "bytes,1,opt,name=name,proto3", "bytes,9,opt,name=name,proto3", 1)},
		{name: "different Go package", source: strings.Replace(string(generated), "package fixture", "package unrelated", 1)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			writeGo(t, root, "config.go", "package fixture\nvar ContractConfig = PluginConfig{}\n")
			writeGo(t, root, "types.pb.go", test.source)
			got, err := Collect(root)
			if err != nil {
				t.Fatalf("Collect() error = %v", err)
			}
			if len(got.Configs) != 1 || got.Configs[0].CanopyType {
				t.Fatalf("unlinked generated type was trusted: %#v", got.Configs)
			}
		})
	}
}

func TestCollectDetectsIndexedMutationAndConfigEscape(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{name: "indexed prefix mutation", body: "func mutate() { ContractConfig.CustomStatePrefixes[0][0] = 7 }\n"},
		{name: "alias escape", body: "func escape() { alias := ContractConfig; _ = alias }\n"},
		{name: "call escape", body: "func consume(*PluginConfig) {}\nfunc escape() { consume(ContractConfig) }\n"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			config := "package fixture\nvar ContractConfig = &PluginConfig{CustomStatePrefixes: [][]byte{{100}}}\n" + test.body
			writeGo(t, root, "config.go", config)
			generated, err := os.ReadFile(filepath.Join(phase2Fixture("pass"), "demo.pb.go"))
			if err != nil {
				t.Fatalf("read generated fixture: %v", err)
			}
			writeGo(t, root, "types.pb.go", string(generated))
			got, err := Collect(root)
			if err != nil {
				t.Fatalf("Collect() error = %v", err)
			}
			if len(got.Configs) != 1 || !got.Configs[0].Mutated || !got.Configs[0].CanopyType {
				t.Fatalf("escape/mutation not classified: %#v", got.Configs)
			}
		})
	}
}

func TestCollectClassifiesMalformedAndUnsupportedSource(t *testing.T) {
	malformed, err := Collect(phase2Fixture("malformed"))
	if err != nil {
		t.Fatalf("Collect(malformed) error = %v", err)
	}
	if malformed.DescriptorCount != 2 || len(malformed.DescriptorIssues) != 1 || !malformed.DescriptorIssues[0].Malformed {
		t.Fatalf("malformed descriptor evidence = %#v", malformed.DescriptorIssues)
	}

	root := t.TempDir()
	writeGo(t, root, "broken.go", "package fixture\nfunc broken(\n")
	got, err := Collect(root)
	if err != nil {
		t.Fatalf("Collect(broken Go) error = %v", err)
	}
	if len(got.Issues) != 1 || got.Issues[0].Kind != "go-parse" {
		t.Fatalf("parse issues = %#v", got.Issues)
	}
}

func TestCollectDoesNotFollowFileReplacedByOutsideSymlink(t *testing.T) {
	root := t.TempDir()
	writeGo(t, root, "config.go", "package fixture\ntype PluginConfig struct{}\nvar ContractConfig = PluginConfig{}\n")
	external := t.TempDir()
	writeGo(t, external, "outside.go", "this is not Go")
	replaced := filepath.Join(root, "linked.go")
	writeGo(t, root, "linked.go", "package fixture\n")
	if err := os.Remove(replaced); err != nil {
		t.Fatalf("remove original file: %v", err)
	}
	if err := os.Symlink(filepath.Join(external, "outside.go"), replaced); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	got, err := Collect(root)
	if err != nil {
		t.Fatalf("Collect() followed or rejected symlink: %v", err)
	}
	if got.SourceFileCount != 1 || len(got.Issues) != 0 {
		t.Fatalf("symlink affected evidence: files=%d issues=%#v", got.SourceFileCount, got.Issues)
	}
}

func TestCollectRejectsOversizedSource(t *testing.T) {
	root := t.TempDir()
	data := append([]byte("package fixture\n"), bytes.Repeat([]byte{' '}, maxFileBytes)...)
	if err := os.WriteFile(filepath.Join(root, "large.go"), data, 0o600); err != nil {
		t.Fatalf("write oversized fixture: %v", err)
	}
	if _, err := Collect(root); err == nil || !strings.Contains(err.Error(), "source file exceeds inspection limit") {
		t.Fatalf("Collect() error = %v, want size-limit error", err)
	}
}

func TestCollectSortsFilesAndIgnoresTestSources(t *testing.T) {
	root := t.TempDir()
	writeGo(t, root, "z.go", "package fixture\ntype PluginConfig struct{}\nvar ContractConfig = PluginConfig{}\n")
	writeGo(t, root, "a.go", "package fixture\nvar ContractConfig = PluginConfig{}\n")
	writeGo(t, root, "ignored_test.go", "package fixture\nvar ContractConfig = PluginConfig{}\n")
	got, err := Collect(root)
	if err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	if len(got.Configs) != 2 || got.Configs[0].Loc.Path != "a.go" || got.Configs[1].Loc.Path != "z.go" {
		t.Fatalf("config order = %#v", got.Configs)
	}
}

func TestCollectCNPY003AccountSchemas(t *testing.T) {
	tests := []struct {
		fixture    string
		message    string
		wantFields []ProtoField
		malformed  bool
	}{
		{fixture: "valid", message: "types.Account", wantFields: []ProtoField{{Name: "nonce", Number: 7, Label: 1, Kind: 4}}},
		{fixture: "wrong-type", message: "types.Account", wantFields: []ProtoField{{Name: "nonce", Number: 7, Label: 1, Kind: 9}}},
		{fixture: "repeated", message: "types.Account", wantFields: []ProtoField{{Name: "nonce", Number: 7, Label: 3, Kind: 4}}},
		{fixture: "omitted", message: "types.Account", wantFields: []ProtoField{}},
		{fixture: "unrelated", message: "other.Account", wantFields: []ProtoField{{Name: "nonce", Number: 7, Label: 1, Kind: 4}}},
		{fixture: "field-10", message: "types.Account", wantFields: []ProtoField{{Name: "nonce", Number: 10, Label: 1, Kind: 4}}},
		{fixture: "malformed", malformed: true},
	}
	for _, test := range tests {
		t.Run(test.fixture, func(t *testing.T) {
			got, err := Collect(filepath.Join("..", "..", "testdata", "phase3", test.fixture))
			if err != nil {
				t.Fatalf("Collect() error = %v", err)
			}
			if len(got.Configs) != 1 || !got.Configs[0].CanopyType {
				t.Fatalf("Canopy config provenance missing: %#v", got.Configs)
			}
			if test.malformed {
				if len(got.DescriptorIssues) != 1 || !got.DescriptorIssues[0].Malformed {
					t.Fatalf("malformed descriptor issues = %#v", got.DescriptorIssues)
				}
				if !got.SchemaSelection.Attempted || !got.SchemaSelection.Proven || len(got.SchemaSelection.Issues) != 1 || !got.SchemaSelection.Issues[0].Malformed {
					t.Fatalf("selected malformed descriptor was not proven invalid: %#v", got.SchemaSelection)
				}
				return
			}
			if !got.SchemaSelection.Attempted || !got.SchemaSelection.Proven {
				t.Fatalf("descriptor selection was not proven: %#v", got.SchemaSelection)
			}
			schemas := got.SelectedSchemas[test.message]
			if len(schemas) != 1 {
				t.Fatalf("schemas[%q] = %#v", test.message, schemas)
			}
			if !reflect.DeepEqual(schemas[0].Fields, test.wantFields) {
				t.Fatalf("fields = %#v, want %#v", schemas[0].Fields, test.wantFields)
			}
			if test.fixture == "unrelated" && len(got.SelectedSchemas["types.Account"]) != 0 {
				t.Fatal("unrelated Account was classified as types.Account")
			}
			if test.fixture == "valid" && len(got.MessageSchemas["types.Account"]) != 2 {
				t.Fatalf("stale descriptor was not retained as unselected evidence: %#v", got.MessageSchemas["types.Account"])
			}
		})
	}
}

func TestCollectCNPY003UnresolvableDescriptorExpression(t *testing.T) {
	got, err := Collect(filepath.Join("..", "..", "testdata", "phase3", "unresolvable"))
	if err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	if len(got.DescriptorIssues) != 1 || got.DescriptorIssues[0].Malformed || got.DescriptorIssues[0].Subject != "Account" {
		t.Fatalf("descriptor issues = %#v", got.DescriptorIssues)
	}
}

func TestCollectCNPY003SelectedGraphValidation(t *testing.T) {
	validMessage := "\x22\x18\x0a\x07Account\x12\x0d\x0a\x05nonce\x18\x07\x20\x01\x28\x04"
	nestedBroken := testMessageDescriptor("Account",
		[][]byte{testFieldDescriptor("nonce", 7, 1, 4, "")},
		[][]byte{testMessageDescriptor("Child", [][]byte{testFieldDescriptor("missing", 1, 1, 11, ".types.Missing")}, nil)})
	tests := []struct {
		name    string
		raw     string
		symbols []string
		extra   string
		want    string
	}{
		{
			name:    "missing dependency",
			raw:     "\x0a\x0bgraph.proto\x12\x05types\x1a\x0dmissing.proto" + validMessage,
			symbols: []string{"File_graph_proto"},
			want:    "selected descriptor dependency is missing from the proven set",
		},
		{
			name: "unresolved message reference",
			raw: "\x0a\x0bgraph.proto\x12\x05types\x22\x37\x0a\x07Account" +
				"\x12\x0d\x0a\x05nonce\x18\x07\x20\x01\x28\x04" +
				"\x12\x1d\x0a\x05owner\x18\x08\x20\x01\x28\x0b\x32\x0e.types.Missing",
			symbols: []string{"File_graph_proto"},
			want:    "selected descriptor contains an unresolved message or enum type reference",
		},
		{
			name: "unresolved enum reference",
			raw: "\x0a\x0bgraph.proto\x12\x05types\x22\x3a\x0a\x07Account" +
				"\x12\x0d\x0a\x05nonce\x18\x07\x20\x01\x28\x04" +
				"\x12\x20\x0a\x04mode\x18\x08\x20\x01\x28\x0e\x32\x12.types.MissingEnum",
			symbols: []string{"File_graph_proto"},
			want:    "selected descriptor contains an unresolved message or enum type reference",
		},
		{
			name:    "unresolved nested message reference",
			raw:     string(testBytesField(1, []byte("graph.proto"))) + string(testBytesField(2, []byte("types"))) + string(testBytesField(4, nestedBroken)),
			symbols: []string{"File_graph_proto"},
			want:    "selected descriptor contains an unresolved message or enum type reference",
		},
		{
			name:    "duplicate file names",
			raw:     "\x0a\x0daccount.proto\x12\x05other\x22\x07\x0a\x05Other",
			symbols: []string{"File_account_proto", "File_graph_proto"},
			want:    "selected descriptor has a missing or duplicate file name",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := copyPhase3Fixture(t, "valid")
			writeGo(t, root, "graph.pb.go", "package fixture\nimport \"google.golang.org/protobuf/reflect/protoreflect\"\nvar File_graph_proto protoreflect.FileDescriptor\nvar file_graph_proto_rawDesc = "+strconv.Quote(test.raw)+"\n"+test.extra)
			writeDescriptorInit(t, root, test.symbols...)
			got, err := Collect(root)
			if err != nil {
				t.Fatalf("Collect() error = %v", err)
			}
			if !got.SchemaSelection.Attempted || !got.SchemaSelection.Proven {
				t.Fatalf("selection not proven: %#v", got.SchemaSelection)
			}
			if !selectionHasIssue(got.SchemaSelection, test.want) {
				t.Fatalf("issue %q missing: %#v", test.want, got.SchemaSelection.Issues)
			}
		})
	}
}

func TestDecodeFileDescriptorRejectsExcessiveMessageNesting(t *testing.T) {
	message := testMessageDescriptor("Leaf", nil, nil)
	for depth := 0; depth < maxProtoDepth; depth++ {
		message = testMessageDescriptor("Nested", nil, [][]byte{message})
	}
	raw := testBytesField(1, []byte("deep.proto"))
	raw = append(raw, testBytesField(2, []byte("types"))...)
	raw = append(raw, testBytesField(4, message)...)
	if _, _, _, _, _, _, err := decodeFileDescriptor(raw); err == nil || !strings.Contains(err.Error(), "nesting depth") {
		t.Fatalf("decodeFileDescriptor() error = %v, want nesting limit", err)
	}
}

func testMessageDescriptor(name string, fields, nested [][]byte) []byte {
	result := testBytesField(1, []byte(name))
	for _, field := range fields {
		result = append(result, testBytesField(2, field)...)
	}
	for _, child := range nested {
		result = append(result, testBytesField(3, child)...)
	}
	return result
}

func testFieldDescriptor(name string, number, label, kind uint64, typeName string) []byte {
	result := testBytesField(1, []byte(name))
	result = append(result, testVarintField(3, number)...)
	result = append(result, testVarintField(4, label)...)
	result = append(result, testVarintField(5, kind)...)
	if typeName != "" {
		result = append(result, testBytesField(6, []byte(typeName))...)
	}
	return result
}

func testBytesField(number uint64, payload []byte) []byte {
	result := testVarint(number<<3 | 2)
	result = append(result, testVarint(uint64(len(payload)))...)
	return append(result, payload...)
}

func testVarintField(number, value uint64) []byte {
	result := testVarint(number << 3)
	return append(result, testVarint(value)...)
}

func testVarint(value uint64) []byte {
	var result []byte
	for value >= 0x80 {
		result = append(result, byte(value)|0x80)
		value >>= 7
	}
	return append(result, byte(value))
}

func TestCollectCNPY003SelectionIsDeterministic(t *testing.T) {
	root := phase3Fixture("valid")
	first, err := Collect(root)
	if err != nil {
		t.Fatalf("first Collect() error = %v", err)
	}
	second, err := Collect(root)
	if err != nil {
		t.Fatalf("second Collect() error = %v", err)
	}
	if !reflect.DeepEqual(first.SelectedSchemas, second.SelectedSchemas) || !reflect.DeepEqual(first.SchemaSelection, second.SchemaSelection) {
		t.Fatal("selected descriptor evidence differs across repeated collection")
	}
}

func TestCollectCNPY003GeneratedInitCallsBeforeDescriptorSlice(t *testing.T) {
	root := copyPhase3Fixture(t, "valid")
	path := filepath.Join(root, "descriptor_init.go")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read descriptor init fixture: %v", err)
	}
	updated := strings.Replace(string(data), "func init() {\n\tvar fds [][]byte", `func file_account_proto_init() {}
func file_plugin_proto_init() {}

func init() {
	file_account_proto_init()
	file_plugin_proto_init()
	var fds [][]byte`, 1)
	if updated == string(data) {
		t.Fatal("descriptor init fixture did not contain the expected pipeline")
	}
	if err := os.WriteFile(path, []byte(updated), 0o600); err != nil {
		t.Fatalf("write generated-style descriptor init fixture: %v", err)
	}

	got, err := Collect(root)
	if err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	if !got.SchemaSelection.Attempted || !got.SchemaSelection.Proven {
		t.Fatalf("generated-style descriptor selection was not proven: %#v", got.SchemaSelection)
	}
	if _, ok := got.SelectedSchemas["types.Account"]; !ok {
		t.Fatalf("types.Account was not selected: %#v", got.SelectedSchemas)
	}
}

func TestCollectCNPY003UnsupportedOrAmbiguousAssignment(t *testing.T) {
	tests := []struct {
		name string
		edit func(*testing.T, string)
	}{
		{
			name: "unsupported helper assignment",
			edit: func(t *testing.T, root string) {
				writeGo(t, root, "descriptor_init.go", "package fixture\nfunc init() { ContractConfig.FileDescriptorProtos = buildDescriptors() }\nfunc buildDescriptors() [][]byte { return nil }\n")
			},
		},
		{
			name: "ambiguous multiple assignments",
			edit: func(t *testing.T, root string) {
				data, err := os.ReadFile(filepath.Join(root, "descriptor_init.go"))
				if err != nil {
					t.Fatalf("read init fixture: %v", err)
				}
				data = append(data, []byte("\nfunc second() { ContractConfig.FileDescriptorProtos = nil }\n")...)
				if err := os.WriteFile(filepath.Join(root, "descriptor_init.go"), data, 0o600); err != nil {
					t.Fatalf("write ambiguous fixture: %v", err)
				}
			},
		},
		{
			name: "extra descriptor slice writer",
			edit: func(t *testing.T, root string) {
				path := filepath.Join(root, "descriptor_init.go")
				data, err := os.ReadFile(path)
				if err != nil {
					t.Fatalf("read init fixture: %v", err)
				}
				data = []byte(strings.Replace(string(data), "ContractConfig.FileDescriptorProtos = fds", "fds = append(fds, []byte{0xff})\n\tContractConfig.FileDescriptorProtos = fds", 1))
				if err := os.WriteFile(path, data, 0o600); err != nil {
					t.Fatalf("write extra-writer fixture: %v", err)
				}
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := copyPhase3Fixture(t, "valid")
			test.edit(t, root)
			got, err := Collect(root)
			if err != nil {
				t.Fatalf("Collect() error = %v", err)
			}
			if !got.SchemaSelection.Attempted || got.SchemaSelection.Proven || len(got.SchemaSelection.Issues) == 0 {
				t.Fatalf("unsupported selection was not preserved for review: %#v", got.SchemaSelection)
			}
		})
	}
}

func TestCollectCNPY003DoesNotTrustDeadNonInitPipeline(t *testing.T) {
	root := copyPhase3Fixture(t, "valid")
	path := filepath.Join(root, "descriptor_init.go")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read init fixture: %v", err)
	}
	data = []byte(strings.Replace(string(data), "func init()", "func assembleButNeverCalled()", 1))
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write non-init fixture: %v", err)
	}
	got, err := Collect(root)
	if err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	if got.SchemaSelection.Proven {
		t.Fatalf("dead non-init descriptor pipeline was trusted: %#v", got.SchemaSelection)
	}
}

func selectionHasIssue(selection DescriptorSelection, detail string) bool {
	for _, issue := range selection.Issues {
		if issue.Malformed && issue.Detail == detail {
			return true
		}
	}
	return false
}

func copyPhase3Fixture(t *testing.T, name string) string {
	t.Helper()
	source := phase3Fixture(name)
	target := t.TempDir()
	entries, err := os.ReadDir(source)
	if err != nil {
		t.Fatalf("read fixture directory: %v", err)
	}
	for _, entry := range entries {
		if entry.IsDir() {
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

func writeDescriptorInit(t *testing.T, root string, symbols ...string) {
	t.Helper()
	body := "package fixture\nimport (\n\"google.golang.org/protobuf/proto\"\n\"google.golang.org/protobuf/reflect/protoreflect\"\n\"google.golang.org/protobuf/reflect/protodesc\"\n)\nfunc init() {\nvar fds [][]byte\nfor _, file := range []protoreflect.FileDescriptor{\n"
	for _, symbol := range symbols {
		body += symbol + ",\n"
	}
	body += "} {\nfd, _ := proto.Marshal(protodesc.ToFileDescriptorProto(file))\nfds = append(fds, fd)\n}\nContractConfig.FileDescriptorProtos = fds\n}\n"
	writeGo(t, root, "descriptor_init.go", body)
}

func phase3Fixture(name string) string {
	return filepath.Join("..", "..", "testdata", "phase3", name)
}

func phase2Fixture(name string) string {
	return filepath.Join("..", "..", "testdata", "phase2", name)
}

func writeGo(t *testing.T, root, name, body string) {
	t.Helper()
	path := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("create fixture directory: %v", err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
}
