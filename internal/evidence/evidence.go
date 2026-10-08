// Package evidence reads bounded, typed evidence from an untrusted Go project.
// It parses source as data and never loads, type-checks, or executes target code.
package evidence

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"unicode"
)

const (
	maxFiles      = 10_000
	maxFileBytes  = 4 << 20
	maxTotal      = 64 << 20
	maxDepth      = 64
	maxProtoItem  = 10_000
	maxProtoDepth = 64
	maxEntries    = 100_000
)

// Location is a project-relative source location.
type Location struct {
	Path string
	Line int
}

// StringList is a supported literal string-list expression.
type StringList struct {
	Values  []string
	Present bool
	Dynamic bool
	Loc     Location
}

// Prefix is one declared custom state prefix.
type Prefix struct {
	Bytes   []byte
	Dynamic bool
	Loc     Location
}

// PrefixList is a supported literal custom-prefix expression.
type PrefixList struct {
	Values  []Prefix
	Present bool
	Dynamic bool
	Loc     Location
}

// Config is one package-level ContractConfig direct PluginConfig composite.
type Config struct {
	Loc                   Location
	SupportedTransactions StringList
	TransactionTypeURLs   StringList
	CustomStatePrefixes   PrefixList
	Dynamic               bool
	Mutated               bool
	CanopyType            bool
	localType             bool
	sourceKey             string
}

// Issue is incomplete source evidence that needs review.
type Issue struct {
	Kind   string
	Loc    Location
	Detail string
}

// DescriptorIssue distinguishes unsupported descriptor syntax from malformed
// protobuf wire data. Malformed wire data is authoritative blocking evidence.
type DescriptorIssue struct {
	Loc       Location
	Detail    string
	Malformed bool
	Subject   string
}

// ProtoField is the bounded subset of FieldDescriptorProto needed by the
// protected-state compatibility rules. Kind and Label use protobuf enum values.
type ProtoField struct {
	Name     string
	Number   uint64
	Label    uint64
	Kind     uint64
	TypeName string
}

// MessageSchema is one top-level protobuf message declaration.
type MessageSchema struct {
	Name   string
	Fields []ProtoField
	Loc    Location
}

// DescriptorSelection is the CNPY003 view of descriptors proven to flow into
// ContractConfig.FileDescriptorProtos through the accepted initialization form.
type DescriptorSelection struct {
	Attempted bool
	Proven    bool
	Loc       Location
	Issues    []DescriptorIssue
}

// Result is the normalized evidence consumed by deterministic rules.
type Result struct {
	Collected             bool
	Configs               []Config
	Messages              map[string]Location
	MessageSchemas        map[string][]MessageSchema
	SelectedSchemas       map[string][]MessageSchema
	SchemaSelection       DescriptorSelection
	DescriptorIssues      []DescriptorIssue
	DescriptorCount       int
	SourceFileCount       int
	Issues                []Issue
	canopyConfigDirs      map[string]bool
	messageCount          int
	rawDescriptors        map[string]map[string]rawDescriptor
	fileDescriptorSymbols map[string]map[string]bool
}

type rawDescriptor struct {
	symbol       string
	loc          Location
	fileName     string
	dependencies []string
	messages     []MessageSchema
	enumNames    []string
	malformed    bool
}

type sourceFile struct {
	rel  string
	data []byte
}

type parsedSource struct {
	file sourceFile
	fset *token.FileSet
	ast  *ast.File
	key  string
}

// Collect recursively reads regular Go files under root within fixed resource
// limits. Symlinks, hidden directories, .git, and vendor are not traversed.
func Collect(root string) (Result, error) {
	files, err := walk(root)
	if err != nil {
		return Result{}, err
	}
	result := Result{Collected: true, Messages: make(map[string]Location), MessageSchemas: make(map[string][]MessageSchema), SelectedSchemas: make(map[string][]MessageSchema), canopyConfigDirs: make(map[string]bool), rawDescriptors: make(map[string]map[string]rawDescriptor), fileDescriptorSymbols: make(map[string]map[string]bool)}
	result.SourceFileCount = len(files)
	configs := make([]Config, 0, 1)
	var parsedFiles []parsedSource
	var flowFiles []parsedSource
	for _, file := range files {
		if strings.HasSuffix(file.rel, ".pb.go") {
			collectDescriptors(file, &result)
			if strings.HasSuffix(file.rel, "_test.go") {
				continue
			}
			fset := token.NewFileSet()
			parsed, parseErr := parser.ParseFile(fset, file.rel, file.data, 0)
			if parseErr == nil {
				flowFiles = append(flowFiles, parsedSource{file: file, fset: fset, ast: parsed, key: filepath.Dir(file.rel) + "\x00" + parsed.Name.Name})
			}
			continue
		}
		if strings.HasSuffix(file.rel, "_test.go") {
			continue
		}
		fset := token.NewFileSet()
		parsed, parseErr := parser.ParseFile(fset, file.rel, file.data, 0)
		if parseErr != nil {
			result.Issues = append(result.Issues, Issue{Kind: "go-parse", Loc: Location{Path: file.rel}, Detail: "Go source could not be parsed"})
			continue
		}
		source := parsedSource{file: file, fset: fset, ast: parsed, key: filepath.Dir(file.rel) + "\x00" + parsed.Name.Name}
		parsedFiles = append(parsedFiles, source)
		flowFiles = append(flowFiles, source)
	}
	flowWitnesses := make(map[string]bool)
	flowPackages := make(map[string][]parsedSource)
	for _, source := range flowFiles {
		flowPackages[source.key] = append(flowPackages[source.key], source)
	}
	for key, packageFiles := range flowPackages {
		flowWitnesses[key] = officialConfigFlow(packageFiles)
	}
	byteVars := make(map[string]map[string]Prefix)
	mutated := make(map[string]bool)
	mutatedNames := make(map[string]map[string]bool)
	for _, source := range parsedFiles {
		if byteVars[source.key] == nil {
			byteVars[source.key] = make(map[string]Prefix)
		}
		mergeByteVars(byteVars[source.key], packageByteVars(source.ast))
		mutated[source.key] = mutated[source.key] || mutatesCheckedConfig(source.ast) || (escapesContractConfig(source.ast) && !flowWitnesses[source.key])
		if mutatedNames[source.key] == nil {
			mutatedNames[source.key] = make(map[string]bool)
		}
		for name := range assignedIdentifiers(source.ast) {
			mutatedNames[source.key][name] = true
		}
	}
	for key, names := range mutatedNames {
		for name := range names {
			if prefix, exists := byteVars[key][name]; exists {
				prefix.Dynamic = true
				byteVars[key][name] = prefix
			}
		}
	}
	for _, source := range parsedFiles {
		file, fset, parsed := source.file, source.fset, source.ast
		for _, declaration := range parsed.Decls {
			gen, ok := declaration.(*ast.GenDecl)
			if !ok {
				continue
			}
			for _, spec := range gen.Specs {
				value, ok := spec.(*ast.ValueSpec)
				if !ok {
					continue
				}
				for index, name := range value.Names {
					if name.Name != "ContractConfig" {
						continue
					}
					if index >= len(value.Values) {
						result.Issues = append(result.Issues, Issue{Kind: "config-shape", Loc: location(fset, file.rel, name.Pos()), Detail: "ContractConfig has no direct initializer"})
						continue
					}
					initializer := value.Values[index]
					if pointer, ok := initializer.(*ast.UnaryExpr); ok && pointer.Op == token.AND {
						initializer = pointer.X
					}
					literal, ok := initializer.(*ast.CompositeLit)
					if !ok || !isPluginConfig(literal.Type) {
						result.Issues = append(result.Issues, Issue{Kind: "config-shape", Loc: location(fset, file.rel, name.Pos()), Detail: "ContractConfig is not a direct PluginConfig composite literal"})
						continue
					}
					config := parseConfig(fset, file.rel, literal, byteVars[source.key])
					config.sourceKey = source.key
					if ident, ok := literal.Type.(*ast.Ident); ok && ident.Name == "PluginConfig" {
						config.localType = true
					}
					config.Mutated = mutated[source.key]
					configs = append(configs, config)
				}
			}
		}
	}
	result.Configs = configs
	for index := range result.Configs {
		if result.Configs[index].localType && result.canopyConfigDirs[result.Configs[index].sourceKey] {
			result.Configs[index].CanopyType = true
		}
	}
	collectSelectedDescriptors(parsedFiles, &result)
	sort.Slice(result.Configs, func(i, j int) bool { return lessLoc(result.Configs[i].Loc, result.Configs[j].Loc) })
	sort.Slice(result.Issues, func(i, j int) bool { return lessLoc(result.Issues[i].Loc, result.Issues[j].Loc) })
	sort.Slice(result.DescriptorIssues, func(i, j int) bool { return lessLoc(result.DescriptorIssues[i].Loc, result.DescriptorIssues[j].Loc) })
	return result, nil
}

func walk(root string) ([]sourceFile, error) {
	var paths []string
	total := int64(0)
	visited := 0
	rootHandle, err := os.OpenRoot(root)
	if err != nil {
		return nil, fmt.Errorf("project root could not be opened safely")
	}
	defer rootHandle.Close()
	err = filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return fmt.Errorf("filesystem traversal failed")
		}
		if path == root {
			return nil
		}
		visited++
		if visited > maxEntries {
			return fmt.Errorf("project exceeds traversal entry limit")
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return fmt.Errorf("path normalization failed")
		}
		if !safeRelative(rel) {
			return fmt.Errorf("source path cannot be represented safely")
		}
		depth := len(strings.Split(filepath.Clean(rel), string(filepath.Separator)))
		if depth > maxDepth {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		name := entry.Name()
		if entry.IsDir() {
			if name == ".git" || name == "vendor" || strings.HasPrefix(name, ".") {
				return filepath.SkipDir
			}
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return fmt.Errorf("file metadata unavailable")
		}
		if !info.Mode().IsRegular() || !strings.HasSuffix(name, ".go") {
			return nil
		}
		if info.Size() > maxFileBytes {
			return fmt.Errorf("source file exceeds inspection limit")
		}
		total += info.Size()
		if total > maxTotal {
			return fmt.Errorf("project exceeds inspection byte limit")
		}
		paths = append(paths, rel)
		if len(paths) > maxFiles {
			return fmt.Errorf("project exceeds inspection file limit")
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(paths)
	files := make([]sourceFile, 0, len(paths))
	for _, rel := range paths {
		file, err := rootHandle.Open(rel)
		if err != nil {
			return nil, fmt.Errorf("source file could not be read")
		}
		info, statErr := file.Stat()
		if statErr != nil || !info.Mode().IsRegular() {
			file.Close()
			return nil, fmt.Errorf("source file changed during inspection")
		}
		data, readErr := io.ReadAll(io.LimitReader(file, maxFileBytes+1))
		file.Close()
		if readErr != nil || len(data) > maxFileBytes {
			return nil, fmt.Errorf("source file could not be read safely")
		}
		files = append(files, sourceFile{rel: filepath.ToSlash(rel), data: data})
	}
	return files, nil
}

func parseConfig(fset *token.FileSet, path string, literal *ast.CompositeLit, byteVars map[string]Prefix) Config {
	config := Config{Loc: location(fset, path, literal.Pos())}
	seen := make(map[string]bool)
	for _, element := range literal.Elts {
		field, ok := element.(*ast.KeyValueExpr)
		if !ok {
			config.Dynamic = true
			continue
		}
		name, ok := field.Key.(*ast.Ident)
		if !ok {
			config.Dynamic = true
			continue
		}
		if seen[name.Name] {
			config.Dynamic = true
		}
		seen[name.Name] = true
		switch name.Name {
		case "SupportedTransactions":
			config.SupportedTransactions = parseStringList(fset, path, field.Value)
		case "TransactionTypeUrls", "TransactionTypeURLs":
			config.TransactionTypeURLs = parseStringList(fset, path, field.Value)
		case "CustomStatePrefixes":
			config.CustomStatePrefixes = parsePrefixList(fset, path, field.Value, byteVars)
		}
	}
	return config
}

func parseStringList(fset *token.FileSet, path string, expression ast.Expr) StringList {
	result := StringList{Present: true, Loc: location(fset, path, expression.Pos())}
	if ident, ok := expression.(*ast.Ident); ok && ident.Name == "nil" {
		return result
	}
	literal, ok := expression.(*ast.CompositeLit)
	if !ok || !isStringSlice(literal.Type) {
		result.Dynamic = true
		return result
	}
	for _, element := range literal.Elts {
		basic, ok := element.(*ast.BasicLit)
		if !ok || basic.Kind != token.STRING {
			result.Dynamic = true
			return result
		}
		value, err := strconv.Unquote(basic.Value)
		if err != nil {
			result.Dynamic = true
			return result
		}
		result.Values = append(result.Values, value)
	}
	return result
}

func parsePrefixList(fset *token.FileSet, path string, expression ast.Expr, byteVars map[string]Prefix) PrefixList {
	result := PrefixList{Present: true, Loc: location(fset, path, expression.Pos())}
	if ident, ok := expression.(*ast.Ident); ok && ident.Name == "nil" {
		return result
	}
	literal, ok := expression.(*ast.CompositeLit)
	if !ok || !isByteSliceSlice(literal.Type) {
		result.Dynamic = true
		return result
	}
	for _, element := range literal.Elts {
		if ident, ok := element.(*ast.Ident); ok {
			// Package variables hold mutable slices. Without type-checking and
			// escape analysis, even a literal initializer cannot prove the bytes
			// that reach the handshake.
			_ = byteVars[ident.Name]
			result.Values = append(result.Values, Prefix{Dynamic: true, Loc: location(fset, path, ident.Pos())})
			continue
		}
		inner, ok := element.(*ast.CompositeLit)
		if !ok || (inner.Type != nil && !isByteSlice(inner.Type)) {
			result.Values = append(result.Values, Prefix{Dynamic: true, Loc: location(fset, path, element.Pos())})
			continue
		}
		result.Values = append(result.Values, parseByteLiteral(fset, path, inner))
	}
	return result
}

func packageByteVars(file *ast.File) map[string]Prefix {
	result := make(map[string]Prefix)
	duplicates := make(map[string]bool)
	fset := token.NewFileSet() // locations are replaced at the use site
	for _, declaration := range file.Decls {
		gen, ok := declaration.(*ast.GenDecl)
		if !ok {
			continue
		}
		for _, spec := range gen.Specs {
			value, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for index, name := range value.Names {
				if index >= len(value.Values) {
					continue
				}
				literal, ok := value.Values[index].(*ast.CompositeLit)
				if !ok || !isByteSlice(literal.Type) {
					continue
				}
				if _, exists := result[name.Name]; exists {
					duplicates[name.Name] = true
				}
				result[name.Name] = parseByteLiteral(fset, "", literal)
			}
		}
	}
	for name := range duplicates {
		result[name] = Prefix{Dynamic: true}
	}
	return result
}

func mergeByteVars(destination, source map[string]Prefix) {
	for name, prefix := range source {
		if _, exists := destination[name]; exists {
			destination[name] = Prefix{Dynamic: true}
			continue
		}
		destination[name] = prefix
	}
}

func parseByteLiteral(fset *token.FileSet, path string, literal *ast.CompositeLit) Prefix {
	result := Prefix{Loc: location(fset, path, literal.Pos())}
	for _, element := range literal.Elts {
		basic, ok := element.(*ast.BasicLit)
		if !ok || basic.Kind != token.INT {
			result.Dynamic = true
			return result
		}
		value, err := strconv.ParseUint(basic.Value, 0, 8)
		if err != nil {
			result.Dynamic = true
			return result
		}
		result.Bytes = append(result.Bytes, byte(value))
	}
	return result
}

func collectDescriptors(file sourceFile, result *Result) {
	fset := token.NewFileSet()
	parsed, err := parser.ParseFile(fset, file.rel, file.data, 0)
	if err != nil {
		result.DescriptorIssues = append(result.DescriptorIssues, DescriptorIssue{Loc: Location{Path: file.rel}, Detail: "generated protobuf source could not be parsed"})
		return
	}
	sourceKey := filepath.Dir(file.rel) + "\x00" + parsed.Name.Name
	if result.rawDescriptors[sourceKey] == nil {
		result.rawDescriptors[sourceKey] = make(map[string]rawDescriptor)
	}
	if result.fileDescriptorSymbols[sourceKey] == nil {
		result.fileDescriptorSymbols[sourceKey] = make(map[string]bool)
	}
	for _, symbol := range generatedFileSymbols(parsed) {
		result.fileDescriptorSymbols[sourceKey][symbol] = true
	}
	for _, declaration := range parsed.Decls {
		gen, ok := declaration.(*ast.GenDecl)
		if !ok {
			continue
		}
		for _, spec := range gen.Specs {
			value, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for index, name := range value.Names {
				if !strings.HasSuffix(name.Name, "_rawDesc") || index >= len(value.Values) {
					continue
				}
				loc := location(fset, file.rel, name.Pos())
				raw, ok := stringExpression(value.Values[index])
				if !ok {
					result.DescriptorIssues = append(result.DescriptorIssues, DescriptorIssue{Loc: loc, Detail: "raw descriptor is not a supported string-literal concatenation", Subject: descriptorSubject(name.Name)})
					continue
				}
				result.DescriptorCount++
				fileName, pkg, dependencies, messages, enums, pluginConfigSchema, decodeErr := decodeFileDescriptor([]byte(raw))
				if decodeErr != nil {
					result.DescriptorIssues = append(result.DescriptorIssues, DescriptorIssue{Loc: loc, Detail: "raw descriptor protobuf wire data is malformed", Malformed: true, Subject: descriptorSubject(name.Name)})
					result.rawDescriptors[sourceKey][name.Name] = rawDescriptor{symbol: name.Name, loc: loc, malformed: true}
					continue
				}
				candidate := rawDescriptor{symbol: name.Name, loc: loc, fileName: fileName, dependencies: dependencies}
				for _, message := range messages {
					full := message.name
					if pkg != "" {
						full = pkg + "." + message.name
					}
					if result.messageCount >= maxProtoItem {
						result.DescriptorIssues = append(result.DescriptorIssues, DescriptorIssue{Loc: loc, Detail: "descriptor message count exceeds inspection limit"})
						return
					}
					result.messageCount++
					result.Messages[full] = loc
					fields := make([]ProtoField, 0, len(message.fields))
					for _, field := range message.fields {
						fields = append(fields, ProtoField{Name: field.name, Number: field.number, Label: field.label, Kind: field.kind, TypeName: field.typeName})
					}
					schema := MessageSchema{Name: full, Fields: fields, Loc: loc}
					result.MessageSchemas[full] = append(result.MessageSchemas[full], schema)
					candidate.messages = append(candidate.messages, schema)
				}
				for _, enum := range enums {
					if pkg != "" {
						enum = pkg + "." + enum
					}
					candidate.enumNames = append(candidate.enumNames, enum)
				}
				result.rawDescriptors[sourceKey][name.Name] = candidate
				if pkg == "types" && pluginConfigSchema && hasGeneratedPluginConfigType(parsed) {
					result.canopyConfigDirs[filepath.Dir(file.rel)+"\x00"+parsed.Name.Name] = true
				}
			}
		}
	}
}

func generatedFileSymbols(file *ast.File) []string {
	var symbols []string
	imports := importBindings(file)
	for _, declaration := range file.Decls {
		gen, ok := declaration.(*ast.GenDecl)
		if !ok || gen.Tok != token.VAR {
			continue
		}
		for _, spec := range gen.Specs {
			value, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for _, name := range value.Names {
				validType := isImportedFileDescriptor(value.Type, imports)
				if validType && strings.HasPrefix(name.Name, "File_") && strings.HasSuffix(name.Name, "_proto") {
					symbols = append(symbols, name.Name)
				}
			}
		}
	}
	sort.Strings(symbols)
	return symbols
}

func isImportedFileDescriptor(expression ast.Expr, imports map[string]string) bool {
	selector, ok := expression.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	alias, ok := selector.X.(*ast.Ident)
	return ok && selector.Sel.Name == "FileDescriptor" && imports[alias.Name] == "google.golang.org/protobuf/reflect/protoreflect"
}

func descriptorSubject(name string) string {
	trimmed := strings.TrimSuffix(strings.TrimPrefix(name, "file_"), "_rawDesc")
	parts := strings.Split(trimmed, "_")
	// protoc-go symbols conventionally end in `<proto basename>_proto_rawDesc`.
	// Strip that generated marker before matching the exact basename rather
	// than accepting an arbitrary occurrence of "account" in the symbol.
	if len(parts) > 0 && strings.EqualFold(parts[len(parts)-1], "proto") {
		parts = parts[:len(parts)-1]
	}
	if len(parts) > 0 && strings.EqualFold(parts[len(parts)-1], "account") {
		return "Account"
	}
	return ""
}

type descriptorPipeline struct {
	loc       Location
	symbols   []string
	trusted   []string
	supported bool
}

func collectSelectedDescriptors(files []parsedSource, result *Result) {
	if len(result.Configs) != 1 || len(result.Issues) != 0 || !result.Configs[0].CanopyType {
		return
	}
	key := result.Configs[0].sourceKey
	var pipelines []descriptorPipeline
	for _, source := range files {
		if source.key != key {
			continue
		}
		pipelines = append(pipelines, descriptorPipelines(source)...)
	}
	if len(pipelines) == 0 {
		return
	}
	result.SchemaSelection.Attempted = true
	result.SchemaSelection.Loc = pipelines[0].loc
	if len(pipelines) != 1 || !pipelines[0].supported {
		result.SchemaSelection.Issues = append(result.SchemaSelection.Issues, DescriptorIssue{Loc: pipelines[0].loc, Detail: "descriptor initialization dataflow is unsupported or ambiguous"})
		return
	}
	pipeline := pipelines[0]
	candidates := result.rawDescriptors[key]
	var selected []rawDescriptor
	for _, symbol := range pipeline.symbols {
		if !result.fileDescriptorSymbols[key][symbol] {
			result.SchemaSelection.Issues = append(result.SchemaSelection.Issues, DescriptorIssue{Loc: pipeline.loc, Detail: "selected local FileDescriptor symbol has no same-package generated declaration"})
			return
		}
		rawName := "file_" + strings.TrimPrefix(symbol, "File_") + "_rawDesc"
		candidate, ok := candidates[rawName]
		if !ok {
			result.SchemaSelection.Issues = append(result.SchemaSelection.Issues, DescriptorIssue{Loc: pipeline.loc, Detail: "selected local descriptor cannot be resolved to generated raw data"})
			return
		}
		if candidate.malformed {
			result.SchemaSelection.Issues = append(result.SchemaSelection.Issues, DescriptorIssue{Loc: candidate.loc, Detail: "selected descriptor protobuf wire data is malformed", Malformed: true, Subject: symbol})
			continue
		}
		selected = append(selected, candidate)
	}
	if hasMalformed(result.SchemaSelection.Issues) {
		result.SchemaSelection.Proven = true
		return
	}
	fileNames := make(map[string]bool)
	for _, trusted := range pipeline.trusted {
		fileNames[trusted] = true
	}
	for _, candidate := range selected {
		if candidate.fileName == "" || fileNames[candidate.fileName] {
			result.SchemaSelection.Issues = append(result.SchemaSelection.Issues, DescriptorIssue{Loc: candidate.loc, Detail: "selected descriptor has a missing or duplicate file name", Malformed: true, Subject: candidate.symbol})
			continue
		}
		fileNames[candidate.fileName] = true
	}
	typeNames := make(map[string]bool)
	for _, candidate := range selected {
		for _, schema := range candidate.messages {
			if typeNames[schema.Name] {
				result.SchemaSelection.Issues = append(result.SchemaSelection.Issues, DescriptorIssue{Loc: schema.Loc, Detail: "selected descriptors contain a duplicate fully qualified message name", Malformed: true, Subject: schema.Name})
			}
			typeNames[schema.Name] = true
		}
		for _, name := range candidate.enumNames {
			if typeNames[name] {
				result.SchemaSelection.Issues = append(result.SchemaSelection.Issues, DescriptorIssue{Loc: candidate.loc, Detail: "selected descriptors contain a duplicate fully qualified type name", Malformed: true, Subject: name})
			}
			typeNames[name] = true
		}
	}
	trustedAny := fileNames["google/protobuf/any.proto"]
	for _, candidate := range selected {
		for _, dependency := range candidate.dependencies {
			if !fileNames[dependency] {
				result.SchemaSelection.Issues = append(result.SchemaSelection.Issues, DescriptorIssue{Loc: candidate.loc, Detail: "selected descriptor dependency is missing from the proven set", Malformed: true, Subject: candidate.symbol})
			}
		}
		for _, schema := range candidate.messages {
			for _, field := range schema.Fields {
				if field.Kind != 11 && field.Kind != 14 {
					continue
				}
				reference := strings.TrimPrefix(field.TypeName, ".")
				if reference == "google.protobuf.Any" && trustedAny {
					continue
				}
				if reference == "" || !typeNames[reference] {
					result.SchemaSelection.Issues = append(result.SchemaSelection.Issues, DescriptorIssue{Loc: schema.Loc, Detail: "selected descriptor contains an unresolved message or enum type reference", Malformed: true, Subject: schema.Name})
				}
			}
			result.SelectedSchemas[schema.Name] = append(result.SelectedSchemas[schema.Name], schema)
		}
	}
	result.SchemaSelection.Proven = true
}

func hasMalformed(issues []DescriptorIssue) bool {
	for _, issue := range issues {
		if issue.Malformed {
			return true
		}
	}
	return false
}

func descriptorPipelines(source parsedSource) []descriptorPipeline {
	imports := importBindings(source.ast)
	var pipelines []descriptorPipeline
	for _, declaration := range source.ast.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok || function.Body == nil {
			continue
		}
		assignments := descriptorAssignments(function.Body)
		validInit := function.Name.Name == "init" && function.Recv == nil && function.Type.Params != nil && len(function.Type.Params.List) == 0 && function.Type.Results == nil
		if !validInit {
			for _, assignment := range assignments {
				pipelines = append(pipelines, descriptorPipeline{loc: location(source.fset, source.file.rel, assignment.Pos())})
			}
			continue
		}
		for _, assignment := range assignments {
			pipeline := descriptorPipeline{loc: location(source.fset, source.file.rel, assignment.Pos())}
			fds, ok := assignedIdentifier(assignment.Rhs)
			if !ok {
				pipelines = append(pipelines, pipeline)
				continue
			}
			ranges := findDescriptorRanges(function.Body, fds, imports)
			if len(ranges) != 1 {
				pipelines = append(pipelines, pipeline)
				continue
			}
			pipeline.symbols, pipeline.trusted, pipeline.supported = ranges[0].symbols, ranges[0].trusted,
				ranges[0].supported && assignment.Pos() > ranges[0].position &&
					initializedByteMatrix(function.Body, fds, ranges[0].position) && cleanDescriptorDataflow(function.Body, fds, ranges[0])
			pipelines = append(pipelines, pipeline)
		}
	}
	return pipelines
}

type rangeEvidence struct {
	position  token.Pos
	end       token.Pos
	symbols   []string
	trusted   []string
	supported bool
}

func descriptorAssignments(body *ast.BlockStmt) []*ast.AssignStmt {
	var result []*ast.AssignStmt
	ast.Inspect(body, func(node ast.Node) bool {
		assignment, ok := node.(*ast.AssignStmt)
		if !ok || len(assignment.Lhs) != 1 || len(assignment.Rhs) != 1 {
			return true
		}
		selector, ok := assignment.Lhs[0].(*ast.SelectorExpr)
		root, rootOK := selectorRoot(selector)
		if ok && rootOK && root == "ContractConfig" && selector.Sel.Name == "FileDescriptorProtos" {
			result = append(result, assignment)
		}
		return true
	})
	return result
}

func assignedIdentifier(expressions []ast.Expr) (string, bool) {
	if len(expressions) != 1 {
		return "", false
	}
	ident, ok := expressions[0].(*ast.Ident)
	return identName(ident), ok
}

func findDescriptorRanges(body *ast.BlockStmt, fds string, imports map[string]string) []rangeEvidence {
	var result []rangeEvidence
	ast.Inspect(body, func(node ast.Node) bool {
		rangeStmt, ok := node.(*ast.RangeStmt)
		if !ok {
			return true
		}
		evidence := rangeEvidence{position: rangeStmt.Pos(), end: rangeStmt.End()}
		value, valueOK := rangeStmt.Value.(*ast.Ident)
		literal, literalOK := rangeStmt.X.(*ast.CompositeLit)
		evidence.supported = valueOK && literalOK && isImportedDescriptorSlice(literal.Type, imports)
		if !evidence.supported {
			result = append(result, evidence)
			return true
		}
		for _, element := range literal.Elts {
			switch item := element.(type) {
			case *ast.Ident:
				if !strings.HasPrefix(item.Name, "File_") || !strings.HasSuffix(item.Name, "_proto") {
					evidence.supported = false
				} else {
					evidence.symbols = append(evidence.symbols, item.Name)
				}
			case *ast.SelectorExpr:
				alias, aliasOK := item.X.(*ast.Ident)
				if !aliasOK || imports[alias.Name] != "google.golang.org/protobuf/types/known/anypb" || item.Sel.Name != "File_google_protobuf_any_proto" {
					evidence.supported = false
				} else {
					evidence.trusted = append(evidence.trusted, "google/protobuf/any.proto")
				}
			default:
				evidence.supported = false
			}
		}
		if !rangeMarshalsAndAppends(rangeStmt.Body, value.Name, fds, imports) {
			evidence.supported = false
		}
		result = append(result, evidence)
		return false
	})
	return result
}

func rangeMarshalsAndAppends(body *ast.BlockStmt, rangeVar, fds string, imports map[string]string) bool {
	marshaled := make(map[string]bool)
	appendCount := 0
	totalAppends := 0
	ast.Inspect(body, func(node ast.Node) bool {
		assignment, ok := node.(*ast.AssignStmt)
		if !ok {
			return true
		}
		for index, rhs := range assignment.Rhs {
			if index < len(assignment.Lhs) && isMarshalDescriptorCall(rhs, rangeVar, imports) {
				if ident, ok := assignment.Lhs[index].(*ast.Ident); ok {
					marshaled[ident.Name] = true
				}
			}
		}
		if len(assignment.Lhs) == 1 && len(assignment.Rhs) == 1 {
			left, leftOK := assignment.Lhs[0].(*ast.Ident)
			call, callOK := assignment.Rhs[0].(*ast.CallExpr)
			fn, fnOK := call.Fun.(*ast.Ident)
			if leftOK && callOK && fnOK && left.Name == fds && fn.Name == "append" && len(call.Args) == 2 && isIdent(call.Args[0], fds) {
				totalAppends++
				if value, ok := call.Args[1].(*ast.Ident); ok && marshaled[value.Name] {
					appendCount++
				}
			}
		}
		return true
	})
	return appendCount == 1 && totalAppends == 1
}

func cleanDescriptorDataflow(body *ast.BlockStmt, fds string, descriptorRange rangeEvidence) bool {
	clean := true
	ast.Inspect(body, func(node ast.Node) bool {
		if !clean {
			return false
		}
		switch value := node.(type) {
		case *ast.AssignStmt:
			for _, rhs := range value.Rhs {
				if isIdent(rhs, fds) && !isDescriptorTargetAssignment(value, fds) {
					clean = false
				}
			}
			for index, lhs := range value.Lhs {
				if !isIdent(lhs, fds) {
					continue
				}
				if index < len(value.Rhs) && value.Pos() < descriptorRange.position && isEmptyComposite(value.Rhs[index], isByteSliceSlice) {
					continue
				}
				if value.Pos() >= descriptorRange.position && value.End() <= descriptorRange.end && isAppendAssignment(value, fds) {
					continue
				}
				clean = false
			}
		case *ast.UnaryExpr:
			if value.Op == token.AND && isIdent(value.X, fds) {
				clean = false
			}
		case *ast.ReturnStmt:
			for _, expression := range value.Results {
				if isIdent(expression, fds) {
					clean = false
				}
			}
		case *ast.ValueSpec:
			for _, expression := range value.Values {
				if isIdent(expression, fds) {
					clean = false
				}
			}
		case *ast.CallExpr:
			fn, isBuiltinAppend := value.Fun.(*ast.Ident)
			for _, argument := range value.Args {
				if isIdent(argument, fds) && (!isBuiltinAppend || fn.Name != "append") {
					clean = false
				}
			}
		}
		return clean
	})
	return clean
}

func isDescriptorTargetAssignment(assignment *ast.AssignStmt, fds string) bool {
	if len(assignment.Lhs) != 1 || len(assignment.Rhs) != 1 || !isIdent(assignment.Rhs[0], fds) {
		return false
	}
	selector, ok := assignment.Lhs[0].(*ast.SelectorExpr)
	root, rootOK := selectorRoot(selector)
	return ok && rootOK && root == "ContractConfig" && selector.Sel.Name == "FileDescriptorProtos"
}

func isAppendAssignment(assignment *ast.AssignStmt, target string) bool {
	if len(assignment.Lhs) != 1 || len(assignment.Rhs) != 1 || !isIdent(assignment.Lhs[0], target) {
		return false
	}
	call, ok := assignment.Rhs[0].(*ast.CallExpr)
	fn, fnOK := call.Fun.(*ast.Ident)
	return ok && fnOK && fn.Name == "append" && len(call.Args) == 2 && isIdent(call.Args[0], target)
}

func isMarshalDescriptorCall(expression ast.Expr, rangeVar string, imports map[string]string) bool {
	call, ok := expression.(*ast.CallExpr)
	if !ok || len(call.Args) != 1 || !selectorCall(call.Fun, imports, "google.golang.org/protobuf/proto", "Marshal") {
		return false
	}
	inner, ok := call.Args[0].(*ast.CallExpr)
	return ok && len(inner.Args) == 1 && isIdent(inner.Args[0], rangeVar) && selectorCall(inner.Fun, imports, "google.golang.org/protobuf/reflect/protodesc", "ToFileDescriptorProto")
}

func initializedByteMatrix(body *ast.BlockStmt, name string, before token.Pos) bool {
	initialized := false
	ast.Inspect(body, func(node ast.Node) bool {
		if node == nil || initialized || node.Pos() >= before {
			return false
		}
		switch value := node.(type) {
		case *ast.AssignStmt:
			for index, lhs := range value.Lhs {
				if index < len(value.Rhs) && isIdent(lhs, name) {
					literal, ok := value.Rhs[index].(*ast.CompositeLit)
					initialized = ok && isByteSliceSlice(literal.Type) && len(literal.Elts) == 0
				}
			}
		case *ast.ValueSpec:
			for index, ident := range value.Names {
				if ident.Name == name && isByteSliceSlice(value.Type) {
					initialized = len(value.Values) == 0 || (index < len(value.Values) && isEmptyComposite(value.Values[index], isByteSliceSlice))
				}
			}
		}
		return !initialized
	})
	return initialized
}

func isEmptyComposite(expression ast.Expr, typeCheck func(ast.Expr) bool) bool {
	literal, ok := expression.(*ast.CompositeLit)
	return ok && typeCheck(literal.Type) && len(literal.Elts) == 0
}

func isImportedDescriptorSlice(expression ast.Expr, imports map[string]string) bool {
	array, ok := expression.(*ast.ArrayType)
	if !ok || array.Len != nil {
		return false
	}
	selector, ok := array.Elt.(*ast.SelectorExpr)
	alias, aliasOK := selector.X.(*ast.Ident)
	return ok && aliasOK && selector.Sel.Name == "FileDescriptor" && imports[alias.Name] == "google.golang.org/protobuf/reflect/protoreflect"
}

func importBindings(file *ast.File) map[string]string {
	result := make(map[string]string)
	for _, spec := range file.Imports {
		path, err := strconv.Unquote(spec.Path.Value)
		if err != nil || spec.Name != nil && (spec.Name.Name == "." || spec.Name.Name == "_") {
			continue
		}
		name := filepath.Base(path)
		if spec.Name != nil {
			name = spec.Name.Name
		}
		if _, exists := result[name]; exists {
			result[name] = ""
		} else {
			result[name] = path
		}
	}
	return result
}

func selectorCall(expression ast.Expr, imports map[string]string, importPath, method string) bool {
	selector, ok := expression.(*ast.SelectorExpr)
	alias, aliasOK := selector.X.(*ast.Ident)
	return ok && aliasOK && selector.Sel.Name == method && imports[alias.Name] == importPath
}

func selectorRoot(selector *ast.SelectorExpr) (string, bool) {
	if selector == nil {
		return "", false
	}
	ident, ok := selector.X.(*ast.Ident)
	return identName(ident), ok
}

func identName(ident *ast.Ident) string {
	if ident == nil {
		return ""
	}
	return ident.Name
}

func isIdent(expression ast.Expr, name string) bool {
	ident, ok := expression.(*ast.Ident)
	return ok && ident.Name == name
}

func stringExpression(expression ast.Expr) (string, bool) {
	switch value := expression.(type) {
	case *ast.BasicLit:
		if value.Kind != token.STRING {
			return "", false
		}
		text, err := strconv.Unquote(value.Value)
		return text, err == nil
	case *ast.BinaryExpr:
		if value.Op != token.ADD {
			return "", false
		}
		left, leftOK := stringExpression(value.X)
		right, rightOK := stringExpression(value.Y)
		return left + right, leftOK && rightOK && len(left)+len(right) <= maxFileBytes
	default:
		return "", false
	}
}

// decodeFileDescriptor decodes only fields needed from FileDescriptorProto:
// package (2) and top-level message_type (4), whose nested name is field 1.

func decodeFileDescriptor(data []byte) (string, string, []string, []decodedMessage, []string, bool, error) {
	var fileName string
	var pkg string
	var dependencies []string
	var messages []decodedMessage
	var enums []string
	pluginConfigSchema := false
	for len(data) > 0 {
		number, wire, payload, rest, err := nextField(data)
		if err != nil {
			return "", "", nil, nil, nil, false, err
		}
		data = rest
		if number == 1 && wire == 2 {
			fileName = string(payload)
		}
		if number == 2 && wire == 2 {
			pkg = string(payload)
		}
		if number == 3 && wire == 2 {
			dependencies = append(dependencies, string(payload))
			if len(dependencies) > maxProtoItem {
				return "", "", nil, nil, nil, false, fmt.Errorf("descriptor dependency count exceeds limit")
			}
		}
		if number == 4 && wire == 2 {
			count := 0
			decoded, nestedEnums, err := descriptorMessage(payload, 1, &count)
			if err != nil {
				return "", "", nil, nil, nil, false, err
			}
			messages = append(messages, decoded...)
			enums = append(enums, nestedEnums...)
			if len(messages) > maxProtoItem || len(enums) > maxProtoItem {
				return "", "", nil, nil, nil, false, fmt.Errorf("descriptor type count exceeds limit")
			}
			if len(decoded) > 0 && decoded[0].name == "PluginConfig" && matchesPluginConfigFields(decoded[0].fields) {
				pluginConfigSchema = true
			}
		}
		if number == 5 && wire == 2 {
			name, err := descriptorName(payload)
			if err != nil {
				return "", "", nil, nil, nil, false, err
			}
			if name != "" {
				enums = append(enums, name)
				if len(enums) > maxProtoItem {
					return "", "", nil, nil, nil, false, fmt.Errorf("descriptor enum count exceeds limit")
				}
			}
		}
	}
	return fileName, pkg, dependencies, messages, enums, pluginConfigSchema, nil
}

type decodedMessage struct {
	name   string
	fields []descriptorField
}

type descriptorField struct {
	name        string
	typeName    string
	number      uint64
	label, kind uint64
}

func descriptorMessage(data []byte, depth int, count *int) ([]decodedMessage, []string, error) {
	if depth > maxProtoDepth {
		return nil, nil, fmt.Errorf("descriptor nesting depth exceeds limit")
	}
	*count++
	if *count > maxProtoItem {
		return nil, nil, fmt.Errorf("descriptor message count exceeds limit")
	}
	var name string
	var fields []descriptorField
	var nestedPayloads [][]byte
	var localEnums []string
	for len(data) > 0 {
		number, wire, payload, rest, err := nextField(data)
		if err != nil {
			return nil, nil, err
		}
		data = rest
		if number == 1 && wire == 2 {
			name = string(payload)
		}
		if number == 2 && wire == 2 {
			field, err := decodeDescriptorField(payload)
			if err != nil {
				return nil, nil, err
			}
			fields = append(fields, field)
			if len(fields) > maxProtoItem {
				return nil, nil, fmt.Errorf("descriptor field count exceeds limit")
			}
		}
		if number == 3 && wire == 2 {
			nestedPayloads = append(nestedPayloads, payload)
		}
		if number == 4 && wire == 2 {
			enumName, err := descriptorName(payload)
			if err != nil {
				return nil, nil, err
			}
			if enumName != "" {
				localEnums = append(localEnums, enumName)
			}
		}
	}
	if name == "" {
		return nil, nil, fmt.Errorf("descriptor message has no name")
	}
	messages := []decodedMessage{{name: name, fields: fields}}
	var enums []string
	for _, enumName := range localEnums {
		enums = append(enums, name+"."+enumName)
	}
	for _, payload := range nestedPayloads {
		children, childEnums, err := descriptorMessage(payload, depth+1, count)
		if err != nil {
			return nil, nil, err
		}
		for _, child := range children {
			child.name = name + "." + child.name
			messages = append(messages, child)
		}
		for _, enumName := range childEnums {
			enums = append(enums, name+"."+enumName)
		}
	}
	return messages, enums, nil
}

func descriptorName(data []byte) (string, error) {
	for len(data) > 0 {
		number, wire, payload, rest, err := nextField(data)
		if err != nil {
			return "", err
		}
		data = rest
		if number == 1 && wire == 2 {
			return string(payload), nil
		}
	}
	return "", nil
}

func decodeDescriptorField(data []byte) (descriptorField, error) {
	var field descriptorField
	for len(data) > 0 {
		number, wire, payload, rest, err := nextField(data)
		if err != nil {
			return descriptorField{}, err
		}
		data = rest
		switch {
		case number == 1 && wire == 2:
			field.name = string(payload)
		case (number == 3 || number == 4 || number == 5) && wire == 0:
			value, _, ok := readVarint(payload)
			if !ok {
				return descriptorField{}, fmt.Errorf("invalid descriptor field")
			}
			switch number {
			case 3:
				field.number = value
			case 4:
				field.label = value
			case 5:
				field.kind = value
			}
		case number == 6 && wire == 2:
			field.typeName = string(payload)
		}
	}
	return field, nil
}

func matchesPluginConfigFields(fields []descriptorField) bool {
	want := map[uint64]descriptorField{
		1: {name: "name", number: 1, label: 1, kind: 9},
		2: {name: "id", number: 2, label: 1, kind: 4},
		3: {name: "version", number: 3, label: 1, kind: 4},
		4: {name: "supported_transactions", number: 4, label: 3, kind: 9},
		5: {name: "file_descriptor_protos", number: 5, label: 3, kind: 12},
		6: {name: "transaction_type_urls", number: 6, label: 3, kind: 9},
		7: {name: "event_type_urls", number: 7, label: 3, kind: 9},
		8: {name: "custom_state_prefixes", number: 8, label: 3, kind: 12},
	}
	if len(fields) != len(want) {
		return false
	}
	for _, field := range fields {
		if expected, ok := want[field.number]; !ok || field != expected {
			return false
		}
	}
	return true
}

func hasGeneratedPluginConfigType(file *ast.File) bool {
	type fieldShape struct {
		typeName string
		protoTag string
	}
	required := map[string]fieldShape{
		"Name":                  {"string", "bytes,1,opt,name=name,proto3"},
		"Id":                    {"uint64", "varint,2,opt,name=id,proto3"},
		"Version":               {"uint64", "varint,3,opt,name=version,proto3"},
		"SupportedTransactions": {"[]string", "bytes,4,rep,name=supported_transactions,json=supportedTransactions,proto3"},
		"FileDescriptorProtos":  {"[][]byte", "bytes,5,rep,name=file_descriptor_protos,json=fileDescriptorProtos,proto3"},
		"TransactionTypeUrls":   {"[]string", "bytes,6,rep,name=transaction_type_urls,json=transactionTypeUrls,proto3"},
		"EventTypeUrls":         {"[]string", "bytes,7,rep,name=event_type_urls,json=eventTypeUrls,proto3"},
		"CustomStatePrefixes":   {"[][]byte", "bytes,8,rep,name=custom_state_prefixes,json=customStatePrefixes,proto3"},
	}
	for _, declaration := range file.Decls {
		gen, ok := declaration.(*ast.GenDecl)
		if !ok || gen.Tok != token.TYPE {
			continue
		}
		for _, spec := range gen.Specs {
			typeSpec, ok := spec.(*ast.TypeSpec)
			if !ok || typeSpec.Name.Name != "PluginConfig" {
				continue
			}
			structure, ok := typeSpec.Type.(*ast.StructType)
			if !ok {
				return false
			}
			found := make(map[string]bool, len(required))
			for _, field := range structure.Fields.List {
				tag := ""
				if field.Tag != nil {
					if unquoted, err := strconv.Unquote(field.Tag.Value); err == nil {
						tag = reflect.StructTag(unquoted).Get("protobuf")
					}
				}
				for _, name := range field.Names {
					if expected, exists := required[name.Name]; exists && goTypeName(field.Type) == expected.typeName && tag == expected.protoTag {
						found[name.Name] = true
					}
				}
			}
			for name := range required {
				if !found[name] {
					return false
				}
			}
			return true
		}
	}
	return false
}

func goTypeName(expression ast.Expr) string {
	switch value := expression.(type) {
	case *ast.Ident:
		return value.Name
	case *ast.ArrayType:
		if value.Len != nil {
			return ""
		}
		return "[]" + goTypeName(value.Elt)
	default:
		return ""
	}
}

func nextField(data []byte) (int, int, []byte, []byte, error) {
	key, used, ok := readVarint(data)
	if !ok || key == 0 {
		return 0, 0, nil, nil, fmt.Errorf("invalid protobuf key")
	}
	data = data[used:]
	number, wire := int(key>>3), int(key&7)
	switch wire {
	case 0:
		_, n, ok := readVarint(data)
		if !ok {
			return 0, 0, nil, nil, fmt.Errorf("invalid protobuf varint")
		}
		return number, wire, data[:n], data[n:], nil
	case 1:
		if len(data) < 8 {
			return 0, 0, nil, nil, fmt.Errorf("invalid protobuf fixed64")
		}
		return number, wire, data[:8], data[8:], nil
	case 2:
		length, n, ok := readVarint(data)
		if !ok || length > uint64(len(data)-n) || length > maxFileBytes {
			return 0, 0, nil, nil, fmt.Errorf("invalid protobuf length")
		}
		end := n + int(length)
		return number, wire, data[n:end], data[end:], nil
	case 5:
		if len(data) < 4 {
			return 0, 0, nil, nil, fmt.Errorf("invalid protobuf fixed32")
		}
		return number, wire, data[:4], data[4:], nil
	default:
		return 0, 0, nil, nil, fmt.Errorf("unsupported protobuf wire type")
	}
}

func readVarint(data []byte) (uint64, int, bool) {
	var result uint64
	for i := 0; i < len(data) && i < 10; i++ {
		value := data[i]
		if i == 9 && value > 1 {
			return 0, 0, false
		}
		result |= uint64(value&0x7f) << (7 * i)
		if value < 0x80 {
			return result, i + 1, true
		}
	}
	return 0, 0, false
}

func isPluginConfig(expression ast.Expr) bool {
	switch value := expression.(type) {
	case *ast.Ident:
		return value.Name == "PluginConfig"
	case *ast.SelectorExpr:
		return value.Sel.Name == "PluginConfig"
	default:
		return false
	}
}

func isStringSlice(expression ast.Expr) bool {
	array, ok := expression.(*ast.ArrayType)
	if !ok || array.Len != nil {
		return false
	}
	ident, ok := array.Elt.(*ast.Ident)
	return ok && ident.Name == "string"
}

func isByteSlice(expression ast.Expr) bool {
	array, ok := expression.(*ast.ArrayType)
	if !ok || array.Len != nil {
		return false
	}
	ident, ok := array.Elt.(*ast.Ident)
	return ok && (ident.Name == "byte" || ident.Name == "uint8")
}

func isByteSliceSlice(expression ast.Expr) bool {
	array, ok := expression.(*ast.ArrayType)
	return ok && array.Len == nil && isByteSlice(array.Elt)
}

// officialConfigFlow recognizes only the package-local pointer flow published
// by the official Canopy Go plugin. It is intentionally syntactic and
// fail-closed: every occurrence of ContractConfig and pluginConfig must belong
// to the witnessed declaration, carrier, handshake, or D015 descriptor setup.
func officialConfigFlow(files []parsedSource) bool {
	if len(files) == 0 {
		return false
	}

	contractUses := 0
	pluginFieldUses := 0
	marshallerUses := 0
	var contractSpec *ast.ValueSpec
	var pluginStruct, wrapperStruct, envelopeStruct *ast.StructType
	var start, handshake, syncSend, asyncSend, protoSend, marshal *ast.FuncDecl
	var protoSendImports, marshalImports map[string]string
	marshallerOK := false
	descriptorAssignments := 0
	descriptorSupported := true

	for _, source := range files {
		imports := importBindings(source.ast)
		for _, declaration := range source.ast.Decls {
			switch value := declaration.(type) {
			case *ast.GenDecl:
				for _, specification := range value.Specs {
					switch spec := specification.(type) {
					case *ast.ValueSpec:
						for _, name := range spec.Names {
							if name.Name == "ContractConfig" {
								if contractSpec != nil {
									return false
								}
								contractSpec = spec
							}
							if name.Name == "marshaller" {
								if marshallerOK || len(spec.Names) != 1 || len(spec.Values) != 1 || !deterministicMarshaller(spec.Values[0], imports) {
									return false
								}
								marshallerOK = true
							}
						}
					case *ast.TypeSpec:
						structure, ok := spec.Type.(*ast.StructType)
						if !ok {
							continue
						}
						switch spec.Name.Name {
						case "Plugin":
							if pluginStruct != nil {
								return false
							}
							pluginStruct = structure
						case "PluginToFSM_Config":
							if wrapperStruct != nil {
								return false
							}
							wrapperStruct = structure
						case "PluginToFSM":
							if envelopeStruct != nil {
								return false
							}
							envelopeStruct = structure
						}
					}
				}
			case *ast.FuncDecl:
				switch value.Name.Name {
				case "StartPlugin":
					if start != nil {
						return false
					}
					start = value
				case "Handshake":
					if handshake != nil {
						return false
					}
					handshake = value
				case "sendToPluginSync":
					if syncSend != nil {
						return false
					}
					syncSend = value
				case "sendToPluginAsync":
					if asyncSend != nil {
						return false
					}
					asyncSend = value
				case "sendProtoMsg":
					if protoSend != nil {
						return false
					}
					protoSend = value
					protoSendImports = imports
				case "Marshal":
					if marshal != nil {
						return false
					}
					marshal = value
					marshalImports = imports
				}
			}
		}
		ast.Inspect(source.ast, func(node ast.Node) bool {
			ident, ok := node.(*ast.Ident)
			if !ok {
				return true
			}
			switch ident.Name {
			case "ContractConfig":
				contractUses++
			case "pluginConfig":
				pluginFieldUses++
			case "marshaller":
				marshallerUses++
			}
			return true
		})
		for _, pipeline := range descriptorPipelines(source) {
			descriptorAssignments++
			descriptorSupported = descriptorSupported && pipeline.supported
		}
	}

	if !directPointerConfig(contractSpec) || contractUses != 2+descriptorAssignments || pluginFieldUses != 3 || marshallerUses != 2 || !marshallerOK {
		return false
	}
	if !officialFunctionMatches(start, "524a2e0ba5dbf5282680a86fd04471cbec7d5696ff91e29451ba383b823515df") ||
		!officialFunctionMatches(handshake, "1544175e965a6d8a750bd93bab21bd28ae8afe94f1601ba5e06caca8997c37a0") ||
		!officialFunctionMatches(syncSend, "5999fa0db0352e0848cf54f1710af195631b9f331ddb832c28d57e7f3906cadf") ||
		!officialFunctionMatches(asyncSend, "6352bb9fdc11b609c66bf89fd0729a22a8150520c21208fb05ebb675dcdb8817") ||
		!officialFunctionMatches(protoSend, "c7cd9286e5ac7ab13f447d4f0f4ee4466b49dd9cc496759b2143eae9b49e0ec6") ||
		!officialFunctionMatches(marshal, "054e38163f2d241c4ec2c57a6c7661b144fb071e87f7a2189d36a238b23c70e9") {
		return false
	}
	if descriptorAssignments > 1 || !descriptorSupported {
		return false
	}
	if !hasExactStructField(pluginStruct, "pluginConfig", pointerTo("PluginConfig"), "") ||
		!hasExactStructField(wrapperStruct, "Config", pointerTo("PluginConfig"), `protobuf:"bytes,2,opt,name=config,proto3,oneof"`) ||
		!hasExactStructField(envelopeStruct, "Payload", namedType("isPluginToFSM_Payload"), `protobuf_oneof:"payload"`) {
		return false
	}
	return validStartFlow(start) && validHandshakeFlow(handshake) &&
		validForwardingMethod(syncSend, "sendToPluginAsync", "request") &&
		validEnvelopeMethod(asyncSend) && validProtoSend(protoSend, protoSendImports) && validMarshal(marshal, marshalImports)
}

// officialFunctionMatches compares a position- and comment-independent AST
// fingerprint with the functions published at the pinned Canopy commit. This
// makes the D020 exception exact: any statement, expression, signature, or
// control-flow change falls back to review.
func officialFunctionMatches(function *ast.FuncDecl, expected string) bool {
	if function == nil {
		return false
	}
	var buffer bytes.Buffer
	positionType := reflect.TypeOf(token.Pos(0))
	err := ast.Fprint(&buffer, nil, function, func(name string, value reflect.Value) bool {
		if name == "Obj" || name == "Scope" || name == "Doc" || name == "Comment" {
			return false
		}
		return value.IsValid() && value.Type() != positionType
	})
	if err != nil {
		return false
	}
	digest := sha256.Sum256(buffer.Bytes())
	return hex.EncodeToString(digest[:]) == expected
}

type typePredicate func(ast.Expr) bool

func pointerTo(name string) typePredicate {
	return func(expression ast.Expr) bool {
		pointer, ok := expression.(*ast.StarExpr)
		ident, identOK := pointerTypeIdent(pointer)
		return ok && identOK && ident.Name == name
	}
}

func pointerTypeIdent(pointer *ast.StarExpr) (*ast.Ident, bool) {
	if pointer == nil {
		return nil, false
	}
	ident, ok := pointer.X.(*ast.Ident)
	return ident, ok
}

func namedType(name string) typePredicate {
	return func(expression ast.Expr) bool {
		ident, ok := expression.(*ast.Ident)
		return ok && ident.Name == name
	}
}

func hasExactStructField(structure *ast.StructType, name string, validType typePredicate, expectedTag string) bool {
	if structure == nil || structure.Fields == nil {
		return false
	}
	matches := 0
	for _, field := range structure.Fields.List {
		for _, fieldName := range field.Names {
			if fieldName.Name != name {
				continue
			}
			matches++
			if len(field.Names) != 1 || !validType(field.Type) {
				return false
			}
			tag := ""
			if field.Tag != nil {
				unquoted, err := strconv.Unquote(field.Tag.Value)
				if err != nil {
					return false
				}
				tag = unquoted
			}
			if expectedTag == "" {
				if tag != "" {
					return false
				}
			} else if tag != expectedTag {
				return false
			}
		}
	}
	return matches == 1
}

func directPointerConfig(spec *ast.ValueSpec) bool {
	if spec == nil || len(spec.Names) != 1 || spec.Names[0].Name != "ContractConfig" || spec.Type != nil || len(spec.Values) != 1 {
		return false
	}
	pointer, ok := spec.Values[0].(*ast.UnaryExpr)
	if !ok || pointer.Op != token.AND {
		return false
	}
	literal, ok := pointer.X.(*ast.CompositeLit)
	if !ok {
		return false
	}
	ident, ok := literal.Type.(*ast.Ident)
	return ok && ident.Name == "PluginConfig"
}

func deterministicMarshaller(expression ast.Expr, imports map[string]string) bool {
	literal, ok := expression.(*ast.CompositeLit)
	if !ok {
		return false
	}
	selector, ok := literal.Type.(*ast.SelectorExpr)
	if !ok || selector.Sel.Name != "MarshalOptions" {
		return false
	}
	alias, ok := selector.X.(*ast.Ident)
	if !ok || imports[alias.Name] != "google.golang.org/protobuf/proto" || len(literal.Elts) != 1 {
		return false
	}
	field, ok := literal.Elts[0].(*ast.KeyValueExpr)
	if !ok {
		return false
	}
	key, keyOK := field.Key.(*ast.Ident)
	value, valueOK := field.Value.(*ast.Ident)
	return keyOK && valueOK && key.Name == "Deterministic" && value.Name == "true"
}

func validStartFlow(function *ast.FuncDecl) bool {
	if function == nil || function.Recv != nil || function.Body == nil {
		return false
	}
	pluginVar := ""
	assignmentPos := token.NoPos
	var pluginAssignment *ast.AssignStmt
	var handshakeCall *ast.CallExpr
	assignments := 0
	calls := 0
	for _, statement := range function.Body.List {
		if assignment, ok := statement.(*ast.AssignStmt); ok && len(assignment.Lhs) == 1 && len(assignment.Rhs) == 1 {
			name, nameOK := assignment.Lhs[0].(*ast.Ident)
			if nameOK && pluginLiteralAssignsConfig(assignment.Rhs[0]) {
				assignments++
				pluginVar = name.Name
				assignmentPos = assignment.Pos()
				pluginAssignment = assignment
			}
		}
		for _, call := range topLevelStatementCalls(statement, true) {
			selector, ok := call.Fun.(*ast.SelectorExpr)
			receiver, receiverOK := selectorRoot(selector)
			if ok && receiverOK && receiver == pluginVar && selector.Sel.Name == "Handshake" && len(call.Args) == 0 && call.Pos() > assignmentPos {
				calls++
				handshakeCall = call
			}
		}
	}
	startCalls := map[string]bool{
		"log.Printf": true, "filepath.Join": true, "time.Tick": true,
		"net.Dial": true, "make": true, pluginVar + ".ListenForInbound": true,
	}
	return assignments == 1 && calls == 1 && noExitBefore(function.Body, handshakeCall.Pos(), startCalls, true, pluginVar) && carrierUseSafe(function.Body, pluginVar, pluginAssignment, handshakeCall)
}

// carrierUseSafe rejects aliases, callbacks, reassignment, and returns before
// the witnessed handshake. The carrier may only be declared, selected through
// its fields/methods, and returned after the handshake completes.
func carrierUseSafe(body *ast.BlockStmt, name string, declaration *ast.AssignStmt, handshake *ast.CallExpr) bool {
	if body == nil || name == "" || declaration == nil || handshake == nil {
		return false
	}
	stack := make([]ast.Node, 0, 16)
	safe := true
	ast.Inspect(body, func(node ast.Node) bool {
		if node == nil {
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
			return safe
		}
		var parent ast.Node
		if len(stack) > 0 {
			parent = stack[len(stack)-1]
		}
		if ident, ok := node.(*ast.Ident); ok && ident.Name == name {
			allowed := false
			switch value := parent.(type) {
			case *ast.SelectorExpr:
				if value.X == ident && (value.Sel.Name == "Handshake" || value.Sel.Name == "ListenForInbound") && len(stack) >= 2 {
					call, ok := stack[len(stack)-2].(*ast.CallExpr)
					allowed = ok && call.Fun == value
				}
			case *ast.AssignStmt:
				allowed = value == declaration && len(value.Lhs) == 1 && value.Lhs[0] == ident
			case *ast.ReturnStmt:
				allowed = ident.Pos() > handshake.Pos()
			}
			if !allowed {
				safe = false
				return false
			}
		}
		stack = append(stack, node)
		return safe
	})
	return safe
}

func pluginLiteralAssignsConfig(expression ast.Expr) bool {
	pointer, ok := expression.(*ast.UnaryExpr)
	if !ok || pointer.Op != token.AND {
		return false
	}
	literal, ok := pointer.X.(*ast.CompositeLit)
	if !ok {
		return false
	}
	typeName, ok := literal.Type.(*ast.Ident)
	if !ok || typeName.Name != "Plugin" {
		return false
	}
	matches := 0
	for _, element := range literal.Elts {
		field, ok := element.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		key, keyOK := field.Key.(*ast.Ident)
		value, valueOK := field.Value.(*ast.Ident)
		if keyOK && key.Name == "pluginConfig" {
			matches++
			if !valueOK || value.Name != "ContractConfig" {
				return false
			}
		}
	}
	return matches == 1
}

func validHandshakeFlow(function *ast.FuncDecl) bool {
	receiver, ok := pointerReceiver(function, "Plugin")
	if !ok || function.Name.Name != "Handshake" {
		return false
	}
	matches := 0
	for _, statement := range function.Body.List {
		for _, call := range topLevelStatementCalls(statement, false) {
			if len(call.Args) > 0 && len(call.Args) <= 2 && methodCall(call.Fun, receiver, "sendToPluginSync") && inlineConfigWrapper(call.Args[len(call.Args)-1], receiver) {
				matches++
			}
		}
	}
	return matches == 1 && noExitBefore(function.Body, firstTopLevelMethodCallPos(function.Body, receiver, "sendToPluginSync"), map[string]bool{"log.Println": true}, false, receiver)
}

func inlineConfigWrapper(expression ast.Expr, receiver string) bool {
	pointer, ok := expression.(*ast.UnaryExpr)
	if !ok || pointer.Op != token.AND {
		return false
	}
	literal, ok := pointer.X.(*ast.CompositeLit)
	if !ok || len(literal.Elts) != 1 {
		return false
	}
	typeName, ok := literal.Type.(*ast.Ident)
	if !ok || typeName.Name != "PluginToFSM_Config" {
		return false
	}
	field, ok := literal.Elts[0].(*ast.KeyValueExpr)
	if !ok {
		return false
	}
	key, keyOK := field.Key.(*ast.Ident)
	selector, valueOK := field.Value.(*ast.SelectorExpr)
	root, rootOK := selectorRoot(selector)
	return keyOK && valueOK && rootOK && key.Name == "Config" && root == receiver && selector.Sel.Name == "pluginConfig"
}

func validForwardingMethod(function *ast.FuncDecl, callee, argument string) bool {
	receiver, ok := pointerReceiver(function, "Plugin")
	if !ok || !hasNamedParam(function, argument, namedType("isPluginToFSM_Payload")) || countIdent(function, argument) != 2 {
		return false
	}
	matches := 0
	for _, statement := range function.Body.List {
		for _, call := range topLevelStatementCalls(statement, false) {
			if !methodCall(call.Fun, receiver, callee) {
				continue
			}
			for _, candidate := range call.Args {
				if ident, ok := candidate.(*ast.Ident); ok && ident.Name == argument {
					matches++
				}
			}
		}
	}
	allowed := map[string]bool{}
	if callee == "sendProtoMsg" {
		allowed["make"] = true
		allowed[receiver+".l.Lock"] = true
		allowed[receiver+".l.Unlock"] = true
	}
	return matches == 1 && noExitBefore(function.Body, firstTopLevelMethodCallPos(function.Body, receiver, callee), allowed, false, receiver)
}

func validEnvelopeMethod(function *ast.FuncDecl) bool {
	receiver, ok := pointerReceiver(function, "Plugin")
	if !ok || !hasNamedParam(function, "request", namedType("isPluginToFSM_Payload")) || countIdent(function, "request") != 2 {
		return false
	}
	matches := 0
	for _, statement := range function.Body.List {
		for _, call := range topLevelStatementCalls(statement, false) {
			if len(call.Args) == 1 && methodCall(call.Fun, receiver, "sendProtoMsg") && inlineEnvelope(call.Args[0]) {
				matches++
			}
		}
	}
	return matches == 1 && noExitBefore(function.Body, firstTopLevelMethodCallPos(function.Body, receiver, "sendProtoMsg"), map[string]bool{
		"make": true, receiver + ".l.Lock": true, receiver + ".l.Unlock": true,
	}, false, receiver)
}

func inlineEnvelope(expression ast.Expr) bool {
	pointer, ok := expression.(*ast.UnaryExpr)
	if !ok || pointer.Op != token.AND {
		return false
	}
	literal, ok := pointer.X.(*ast.CompositeLit)
	if !ok {
		return false
	}
	typeName, ok := literal.Type.(*ast.Ident)
	if !ok || typeName.Name != "PluginToFSM" {
		return false
	}
	matches := 0
	for _, element := range literal.Elts {
		field, ok := element.(*ast.KeyValueExpr)
		if !ok {
			return false
		}
		key, keyOK := field.Key.(*ast.Ident)
		if !keyOK || key.Name != "Payload" {
			continue
		}
		matches++
		value, valueOK := field.Value.(*ast.Ident)
		if !valueOK || value.Name != "request" {
			return false
		}
	}
	return matches == 1
}

func validProtoSend(function *ast.FuncDecl, imports map[string]string) bool {
	receiver, ok := pointerReceiver(function, "Plugin")
	if !ok {
		return false
	}
	message, ok := singleNamedParam(function, importedSelectorType(imports, "google.golang.org/protobuf/proto", "Message"))
	if !ok || countIdent(function, message) != 2 {
		return false
	}
	if len(function.Body.List) != 3 {
		return false
	}
	marshalAssignments := 0
	sends := 0
	marshalPos := token.NoPos
	sendPos := token.NoPos
	for _, statement := range function.Body.List {
		if assignment, ok := statement.(*ast.AssignStmt); ok && len(assignment.Lhs) >= 1 && len(assignment.Rhs) == 1 {
			bz, bzOK := assignment.Lhs[0].(*ast.Ident)
			call, callOK := assignment.Rhs[0].(*ast.CallExpr)
			fun, funOK := callIdent(call)
			if bzOK && callOK && funOK && bz.Name == "bz" && fun.Name == "Marshal" && oneIdentArg(call, message) {
				marshalAssignments++
				marshalPos = assignment.Pos()
			}
		}
		for _, call := range topLevelStatementCalls(statement, false) {
			if methodCall(call.Fun, receiver, "sendLengthPrefixed") && oneIdentArg(call, "bz") {
				sends++
				sendPos = call.Pos()
			}
		}
	}
	assignment, assignmentOK := function.Body.List[0].(*ast.AssignStmt)
	if !assignmentOK || len(assignment.Lhs) < 2 {
		return false
	}
	errName, errOK := assignment.Lhs[1].(*ast.Ident)
	return marshalAssignments == 1 && sends == 1 && marshalPos < sendPos && errOK && validErrorGuard(function.Body.List[1], errName.Name, "") &&
		countIdent(function, message) == 2 && countIdent(function, "bz") == 2
}

func validMarshal(function *ast.FuncDecl, imports map[string]string) bool {
	if function == nil || function.Recv != nil || function.Body == nil || !hasNamedParam(function, "message", namedType("any")) || countIdent(function, "message") != 2 {
		return false
	}
	if len(function.Body.List) != 3 {
		return false
	}
	matches := 0
	bytesName := ""
	for _, statement := range function.Body.List {
		assignment, ok := statement.(*ast.AssignStmt)
		if !ok || len(assignment.Lhs) < 1 || len(assignment.Rhs) != 1 {
			continue
		}
		call, ok := assignment.Rhs[0].(*ast.CallExpr)
		if !ok || len(call.Args) != 1 || !methodCall(call.Fun, "marshaller", "Marshal") {
			continue
		}
		assertion, ok := call.Args[0].(*ast.TypeAssertExpr)
		if !ok {
			continue
		}
		value, valueOK := assertion.X.(*ast.Ident)
		if valueOK && value.Name == "message" && importedSelectorType(imports, "google.golang.org/protobuf/proto", "Message")(assertion.Type) {
			bytesIdent, bytesOK := assignment.Lhs[0].(*ast.Ident)
			if bytesOK {
				matches++
				bytesName = bytesIdent.Name
			}
		}
	}
	if matches != 1 || bytesName == "" || countIdent(function, bytesName) != 2 {
		return false
	}
	returned := 0
	for _, candidate := range function.Body.List {
		statement, ok := candidate.(*ast.ReturnStmt)
		if !ok || len(statement.Results) == 0 {
			continue
		}
		if ident, ok := statement.Results[0].(*ast.Ident); ok && ident.Name == bytesName {
			returned++
		}
	}
	assignment, assignmentOK := function.Body.List[0].(*ast.AssignStmt)
	if !assignmentOK || len(assignment.Lhs) < 2 {
		return false
	}
	errName, errOK := assignment.Lhs[1].(*ast.Ident)
	finalReturn, finalOK := function.Body.List[2].(*ast.ReturnStmt)
	if !finalOK || len(finalReturn.Results) != 2 {
		return false
	}
	finalBytes, bytesOK := finalReturn.Results[0].(*ast.Ident)
	finalNil, nilOK := finalReturn.Results[1].(*ast.Ident)
	return returned == 1 && errOK && bytesOK && nilOK && finalBytes.Name == bytesName && finalNil.Name == "nil" &&
		validErrorGuard(function.Body.List[1], errName.Name, "ErrMarshal")
}

func validErrorGuard(statement ast.Stmt, errName, wrapper string) bool {
	guard, ok := statement.(*ast.IfStmt)
	if !ok || guard.Init != nil || guard.Else != nil || guard.Body == nil || len(guard.Body.List) != 1 {
		return false
	}
	condition, ok := guard.Cond.(*ast.BinaryExpr)
	if !ok || condition.Op != token.NEQ {
		return false
	}
	left, leftOK := condition.X.(*ast.Ident)
	right, rightOK := condition.Y.(*ast.Ident)
	if !leftOK || !rightOK || left.Name != errName || right.Name != "nil" {
		return false
	}
	result, ok := guard.Body.List[0].(*ast.ReturnStmt)
	if !ok {
		return false
	}
	if wrapper == "" {
		ident, ok := singleReturnIdent(result)
		return ok && ident.Name == errName
	}
	if len(result.Results) != 2 {
		return false
	}
	first, firstOK := result.Results[0].(*ast.Ident)
	call, callOK := result.Results[1].(*ast.CallExpr)
	fun, funOK := callIdent(call)
	return firstOK && first.Name == "nil" && callOK && funOK && fun.Name == wrapper && oneIdentArg(call, errName)
}

func singleReturnIdent(statement *ast.ReturnStmt) (*ast.Ident, bool) {
	if statement == nil || len(statement.Results) != 1 {
		return nil, false
	}
	ident, ok := statement.Results[0].(*ast.Ident)
	return ident, ok
}

func firstTopLevelMethodCallPos(body *ast.BlockStmt, receiver, method string) token.Pos {
	if body == nil {
		return token.NoPos
	}
	for _, statement := range body.List {
		for _, call := range topLevelStatementCalls(statement, false) {
			if methodCall(call.Fun, receiver, method) {
				return call.Pos()
			}
		}
	}
	return token.NoPos
}

// noExitBefore rules out an earlier function exit or jump that would make a
// syntactically present handoff unreachable. Loop break/continue are allowed;
// they do not leave the function and occur in the official connection loop.
func noExitBefore(body *ast.BlockStmt, required token.Pos, allowedCalls map[string]bool, startFlow bool, receiver string) bool {
	if body == nil || required == token.NoPos {
		return false
	}
	for _, statement := range body.List {
		if required >= statement.Pos() && required < statement.End() {
			return true
		}
		if statement.Pos() >= required {
			return true
		}
		switch value := statement.(type) {
		case *ast.DeclStmt, *ast.AssignStmt, *ast.ExprStmt, *ast.EmptyStmt:
			if containsReceive(statement) || !onlyAllowedCalls(statement, allowedCalls) {
				return false
			}
		case *ast.GoStmt:
			if !startFlow || value.Call == nil || callPath(value.Call.Fun) != receiver+".ListenForInbound" || len(value.Call.Args) != 0 {
				return false
			}
		case *ast.RangeStmt:
			if !startFlow || !officialConnectionLoop(value) {
				return false
			}
		default:
			return false
		}
	}
	return false
}

func onlyAllowedCalls(node ast.Node, allowed map[string]bool) bool {
	safe := true
	ast.Inspect(node, func(candidate ast.Node) bool {
		if !safe {
			return false
		}
		if call, ok := candidate.(*ast.CallExpr); ok && !allowed[callPath(call.Fun)] {
			safe = false
			return false
		}
		return true
	})
	return safe
}

func containsReceive(node ast.Node) bool {
	found := false
	ast.Inspect(node, func(candidate ast.Node) bool {
		if unary, ok := candidate.(*ast.UnaryExpr); ok && unary.Op == token.ARROW {
			found = true
			return false
		}
		return !found
	})
	return found
}

func officialConnectionLoop(loop *ast.RangeStmt) bool {
	if loop == nil || loop.Key != nil || loop.Value != nil || loop.Tok != token.ILLEGAL || loop.Body == nil || len(loop.Body.List) != 4 {
		return false
	}
	tick, ok := loop.X.(*ast.CallExpr)
	if !ok || callPath(tick.Fun) != "time.Tick" || len(tick.Args) != 1 {
		return false
	}
	period, ok := tick.Args[0].(*ast.SelectorExpr)
	if !ok || callPath(period) != "time.Second" {
		return false
	}
	if _, ok := loop.Body.List[0].(*ast.DeclStmt); !ok {
		return false
	}
	assignment, ok := loop.Body.List[1].(*ast.AssignStmt)
	if !ok || len(assignment.Rhs) != 1 {
		return false
	}
	dial, ok := assignment.Rhs[0].(*ast.CallExpr)
	if !ok || callPath(dial.Fun) != "net.Dial" {
		return false
	}
	guard, ok := loop.Body.List[2].(*ast.IfStmt)
	if !ok || guard.Init != nil || guard.Else != nil || guard.Body == nil || len(guard.Body.List) != 1 {
		return false
	}
	condition, ok := guard.Cond.(*ast.BinaryExpr)
	if !ok || condition.Op != token.EQL {
		return false
	}
	left, leftOK := condition.X.(*ast.Ident)
	right, rightOK := condition.Y.(*ast.Ident)
	branch, branchOK := guard.Body.List[0].(*ast.BranchStmt)
	if !leftOK || !rightOK || left.Name != "err" || right.Name != "nil" || !branchOK || branch.Tok != token.BREAK {
		return false
	}
	logStatement, ok := loop.Body.List[3].(*ast.ExprStmt)
	if !ok {
		return false
	}
	logCall, ok := logStatement.X.(*ast.CallExpr)
	return ok && callPath(logCall.Fun) == "log.Printf"
}

func callPath(expression ast.Expr) string {
	switch value := expression.(type) {
	case *ast.Ident:
		return value.Name
	case *ast.SelectorExpr:
		prefix := callPath(value.X)
		if prefix == "" {
			return ""
		}
		return prefix + "." + value.Sel.Name
	default:
		return ""
	}
}

// topLevelStatementCalls returns only calls that are direct parts of a
// function's top-level statement. Calls hidden in branch bodies are excluded.
// StartPlugin additionally permits the official `if err := p.Handshake()` init.
func topLevelStatementCalls(statement ast.Stmt, allowIfInit bool) []*ast.CallExpr {
	var expressions []ast.Expr
	switch value := statement.(type) {
	case *ast.ExprStmt:
		expressions = append(expressions, value.X)
	case *ast.AssignStmt:
		expressions = append(expressions, value.Rhs...)
	case *ast.ReturnStmt:
		expressions = append(expressions, value.Results...)
	case *ast.IfStmt:
		if allowIfInit && value.Init != nil {
			return topLevelStatementCalls(value.Init, false)
		}
	}
	calls := make([]*ast.CallExpr, 0, len(expressions))
	for _, expression := range expressions {
		if call, ok := expression.(*ast.CallExpr); ok {
			calls = append(calls, call)
		}
	}
	return calls
}

func pointerReceiver(function *ast.FuncDecl, typeName string) (string, bool) {
	if function == nil || function.Body == nil || function.Recv == nil || len(function.Recv.List) != 1 {
		return "", false
	}
	field := function.Recv.List[0]
	if len(field.Names) != 1 || !pointerTo(typeName)(field.Type) {
		return "", false
	}
	return field.Names[0].Name, true
}

func hasNamedParam(function *ast.FuncDecl, name string, validType typePredicate) bool {
	if function == nil || function.Type.Params == nil {
		return false
	}
	matches := 0
	for _, field := range function.Type.Params.List {
		for _, fieldName := range field.Names {
			if fieldName.Name == name {
				matches++
				if len(field.Names) != 1 || !validType(field.Type) {
					return false
				}
			}
		}
	}
	return matches == 1
}

func singleNamedParam(function *ast.FuncDecl, validType typePredicate) (string, bool) {
	if function == nil || function.Type.Params == nil || len(function.Type.Params.List) != 1 {
		return "", false
	}
	field := function.Type.Params.List[0]
	if len(field.Names) != 1 || !validType(field.Type) {
		return "", false
	}
	return field.Names[0].Name, true
}

func importedSelectorType(imports map[string]string, importPath, name string) typePredicate {
	return func(expression ast.Expr) bool {
		selector, ok := expression.(*ast.SelectorExpr)
		alias, aliasOK := selectorRoot(selector)
		return ok && aliasOK && selector.Sel.Name == name && imports[alias] == importPath
	}
}

func methodCall(expression ast.Expr, receiver, method string) bool {
	selector, ok := expression.(*ast.SelectorExpr)
	root, rootOK := selectorRoot(selector)
	return ok && rootOK && root == receiver && selector.Sel.Name == method
}

func countIdent(node ast.Node, name string) int {
	count := 0
	ast.Inspect(node, func(candidate ast.Node) bool {
		if ident, ok := candidate.(*ast.Ident); ok && ident.Name == name {
			count++
		}
		return true
	})
	return count
}

func callIdent(call *ast.CallExpr) (*ast.Ident, bool) {
	if call == nil {
		return nil, false
	}
	ident, ok := call.Fun.(*ast.Ident)
	return ident, ok
}

func oneIdentArg(call *ast.CallExpr, name string) bool {
	if call == nil || len(call.Args) != 1 {
		return false
	}
	ident, ok := call.Args[0].(*ast.Ident)
	return ok && ident.Name == name
}

func mutatesCheckedConfig(file *ast.File) bool {
	mutated := false
	ast.Inspect(file, func(node ast.Node) bool {
		switch value := node.(type) {
		case *ast.AssignStmt:
			for _, target := range value.Lhs {
				if checkedConfigTarget(target) {
					mutated = true
				}
			}
		case *ast.IncDecStmt:
			if checkedConfigTarget(value.X) {
				mutated = true
			}
		case *ast.UnaryExpr:
			if value.Op == token.AND && checkedConfigTarget(value.X) {
				mutated = true
			}
		}
		return !mutated
	})
	return mutated
}

func assignedIdentifiers(file *ast.File) map[string]bool {
	result := make(map[string]bool)
	ast.Inspect(file, func(node ast.Node) bool {
		assignment, ok := node.(*ast.AssignStmt)
		if !ok {
			return true
		}
		for _, expression := range assignment.Lhs {
			if ident := rootIdentifier(expression); ident != nil {
				result[ident.Name] = true
			}
		}
		return true
	})
	return result
}

func rootIdentifier(expression ast.Expr) *ast.Ident {
	switch value := expression.(type) {
	case *ast.Ident:
		return value
	case *ast.IndexExpr:
		return rootIdentifier(value.X)
	case *ast.SelectorExpr:
		return rootIdentifier(value.X)
	default:
		return nil
	}
}

func escapesContractConfig(file *ast.File) bool {
	escaped := false
	contains := func(expression ast.Expr) bool {
		found := false
		ast.Inspect(expression, func(node ast.Node) bool {
			if ident, ok := node.(*ast.Ident); ok && ident.Name == "ContractConfig" {
				found = true
				return false
			}
			return !found
		})
		return found
	}
	ast.Inspect(file, func(node ast.Node) bool {
		switch value := node.(type) {
		case *ast.AssignStmt:
			for _, expression := range value.Rhs {
				escaped = escaped || contains(expression)
			}
		case *ast.ValueSpec:
			for _, expression := range value.Values {
				escaped = escaped || contains(expression)
			}
		case *ast.CallExpr:
			for _, expression := range value.Args {
				escaped = escaped || contains(expression)
			}
		case *ast.ReturnStmt:
			for _, expression := range value.Results {
				escaped = escaped || contains(expression)
			}
		}
		return !escaped
	})
	return escaped
}

func checkedConfigTarget(expression ast.Expr) bool {
	switch value := expression.(type) {
	case *ast.Ident:
		return value.Name == "ContractConfig"
	case *ast.SelectorExpr:
		if root, ok := value.X.(*ast.Ident); ok && root.Name == "ContractConfig" {
			switch value.Sel.Name {
			case "SupportedTransactions", "TransactionTypeUrls", "TransactionTypeURLs", "CustomStatePrefixes":
				return true
			default:
				return false
			}
		}
		return checkedConfigTarget(value.X)
	case *ast.IndexExpr:
		return checkedConfigTarget(value.X)
	default:
		return false
	}
}

func location(fset *token.FileSet, path string, pos token.Pos) Location {
	line := 0
	if pos.IsValid() {
		line = fset.Position(pos).Line
	}
	return Location{Path: path, Line: line}
}

func lessLoc(left, right Location) bool {
	if left.Path != right.Path {
		return left.Path < right.Path
	}
	return left.Line < right.Line
}

func safeRelative(value string) bool {
	if value == "" || filepath.IsAbs(value) || strings.Contains(value, "\\") || strings.IndexFunc(value, unicode.IsControl) >= 0 {
		return false
	}
	clean := filepath.Clean(value)
	return clean == value && clean != ".." && !strings.HasPrefix(clean, ".."+string(filepath.Separator))
}

// PrefixHex returns a bounded stable byte-sequence representation.
func PrefixHex(value []byte) string { return "0x" + hex.EncodeToString(value) }
