package rules

import (
	"reflect"
	"testing"

	"github.com/jerrygeorge360/canopy-preflight/internal/diagnostic"
	"github.com/jerrygeorge360/canopy-preflight/internal/evidence"
)

func TestCNPY001RegistryOutcomes(t *testing.T) {
	tests := []struct {
		name  string
		input evidence.Result
		want  diagnostic.Decision
		codes []string
	}{
		{name: "empty registry passes", input: oneConfig(nil, nil), want: diagnostic.DecisionPass},
		{name: "resolved URL still needs handshake provenance", input: registry([]string{"send"}, []string{"type.googleapis.com/demo.MessageSend"}, 1, "demo.MessageSend"), want: diagnostic.DecisionReview, codes: []string{"CNPY001-DESCRIPTOR-REVIEW"}},
		{name: "single component message name is valid", input: registry([]string{"send"}, []string{"MessageSend"}, 1, "MessageSend"), want: diagnostic.DecisionReview, codes: []string{"CNPY001-DESCRIPTOR-REVIEW"}},
		{name: "count mismatch blocks", input: oneConfig([]string{"send", "reward"}, []string{"demo.MessageSend"}), want: diagnostic.DecisionBlock, codes: []string{"CNPY001-COUNT"}},
		{name: "empty URL blocks", input: registry([]string{"send"}, []string{""}, 1, "demo.MessageSend"), want: diagnostic.DecisionBlock, codes: []string{"CNPY001-DESCRIPTOR-REVIEW", "CNPY001-TYPE-URL"}},
		{name: "malformed URL blocks", input: registry([]string{"send"}, []string{"Message-Send"}, 1, "demo.MessageSend"), want: diagnostic.DecisionBlock, codes: []string{"CNPY001-DESCRIPTOR-REVIEW", "CNPY001-TYPE-URL"}},
		{name: "unresolved URL reviews without handshake provenance", input: registry([]string{"send"}, []string{"demo.MessageMissing"}, 1, "demo.MessageSend"), want: diagnostic.DecisionReview, codes: []string{"CNPY001-DESCRIPTOR-REVIEW", "CNPY001-DESCRIPTOR-REVIEW"}},
		{name: "descriptors absent reviews", input: oneConfig([]string{"send"}, []string{"demo.MessageSend"}), want: diagnostic.DecisionReview, codes: []string{"CNPY001-DESCRIPTOR-REVIEW"}},
		{name: "unsupported descriptor reviews", input: withDescriptorIssue(registry([]string{"send"}, []string{"demo.MessageSend"}, 1, "demo.MessageSend"), false), want: diagnostic.DecisionReview, codes: []string{"CNPY001-DESCRIPTOR-REVIEW", "CNPY001-DESCRIPTOR-REVIEW"}},
		{name: "malformed descriptor reviews without handshake provenance", input: withDescriptorIssue(registry([]string{"send"}, []string{"demo.MessageSend"}, 1), true), want: diagnostic.DecisionReview, codes: []string{"CNPY001-DESCRIPTOR-REVIEW", "CNPY001-DESCRIPTOR-REVIEW", "CNPY001-DESCRIPTOR-REVIEW"}},
		{name: "duplicate names review", input: registry([]string{"send", "send"}, []string{"demo.MessageSend", "demo.MessageReward"}, 1, "demo.MessageSend", "demo.MessageReward"), want: diagnostic.DecisionReview, codes: []string{"CNPY001-DESCRIPTOR-REVIEW", "CNPY001-DUPLICATE-REVIEW"}},
		{name: "duplicate URLs review", input: registry([]string{"send", "alias"}, []string{"demo.MessageSend", "demo.MessageSend"}, 1, "demo.MessageSend"), want: diagnostic.DecisionReview, codes: []string{"CNPY001-DESCRIPTOR-REVIEW", "CNPY001-DUPLICATE-REVIEW"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			findings := Evaluate(test.input)
			if got := decision(t, findings); got != test.want {
				t.Fatalf("decision = %q, want %q; findings=%#v", got, test.want, findings)
			}
			if test.codes != nil {
				if got := findingCodes(findings); !reflect.DeepEqual(got, test.codes) {
					t.Fatalf("codes = %v, want %v", got, test.codes)
				}
			}
		})
	}
}

func TestUnprovenPluginConfigIdentityCannotBlock(t *testing.T) {
	input := oneConfig([]string{"send", "reward"}, []string{"demo.MessageSend"})
	input.Configs[0].CanopyType = false
	findings := Evaluate(input)
	if got := decision(t, findings); got != diagnostic.DecisionReview {
		t.Fatalf("decision = %q, want review; findings=%#v", got, findings)
	}
	if got := findingCodes(findings); !reflect.DeepEqual(got, []string{"CNPY001-CONFIG-REVIEW", "CNPY002-PREFIX-REVIEW"}) {
		t.Fatalf("codes = %v", got)
	}
}

func TestCNPY002PrefixBoundaries(t *testing.T) {
	tests := []struct {
		name   string
		prefix evidence.Prefix
		want   diagnostic.Decision
		code   string
	}{
		{name: "zero", prefix: literalPrefix(0), want: diagnostic.DecisionPass},
		{name: "one", prefix: literalPrefix(1), want: diagnostic.DecisionBlock, code: "CNPY002-DECLARED-COLLISION"},
		{name: "seven", prefix: literalPrefix(7), want: diagnostic.DecisionBlock, code: "CNPY002-DECLARED-COLLISION"},
		{name: "fifteen", prefix: literalPrefix(15), want: diagnostic.DecisionBlock, code: "CNPY002-DECLARED-COLLISION"},
		{name: "sixteen", prefix: literalPrefix(16), want: diagnostic.DecisionPass},
		{name: "two hundred fifty five", prefix: literalPrefix(255), want: diagnostic.DecisionPass},
		{name: "multi byte beginning reserved", prefix: literalPrefix(1, 2), want: diagnostic.DecisionPass},
		{name: "empty", prefix: literalPrefix(), want: diagnostic.DecisionReview, code: "CNPY002-PREFIX-REVIEW"},
		{name: "dynamic", prefix: evidence.Prefix{Dynamic: true, Loc: testLocation()}, want: diagnostic.DecisionReview, code: "CNPY002-PREFIX-REVIEW"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			input := oneConfig(nil, nil)
			input.Configs[0].CustomStatePrefixes = evidence.PrefixList{Present: true, Values: []evidence.Prefix{test.prefix}, Loc: testLocation()}
			findings := Evaluate(input)
			if got := decision(t, findings); got != test.want {
				t.Fatalf("decision = %q, want %q; findings=%#v", got, test.want, findings)
			}
			if test.code != "" && (len(findings) != 1 || findings[0].Code != test.code) {
				t.Fatalf("findings = %#v, want code %q", findings, test.code)
			}
		})
	}
}

func TestAmbiguousEvidenceReviewsInsteadOfBlocking(t *testing.T) {
	tests := []struct {
		name  string
		input evidence.Result
	}{
		{name: "dynamic registry", input: dynamicRegistry()},
		{name: "dynamic prefix list", input: dynamicPrefixes()},
		{name: "mutated config", input: mutatedConfig()},
		{name: "multiple configs", input: evidence.Result{SourceFileCount: 2, Messages: map[string]evidence.Location{}, Configs: []evidence.Config{{Loc: evidence.Location{Path: "b.go", Line: 2}}, {Loc: evidence.Location{Path: "a.go", Line: 2}}}}},
		{name: "malformed Go issue suppresses blocking checks", input: evidence.Result{SourceFileCount: 1, Messages: map[string]evidence.Location{}, Configs: []evidence.Config{oneConfig([]string{"a"}, nil).Configs[0]}, Issues: []evidence.Issue{{Kind: "go-parse", Loc: evidence.Location{Path: "broken.go"}, Detail: "Go source could not be parsed"}}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			findings := Evaluate(test.input)
			if got := decision(t, findings); got != diagnostic.DecisionReview {
				t.Fatalf("decision = %q, want review; findings=%#v", got, findings)
			}
		})
	}
}

func TestEvaluateFindingsAreDeterministicallySorted(t *testing.T) {
	input := registry([]string{"send", "send"}, []string{"bad", "bad"}, 1)
	input.DescriptorIssues = []evidence.DescriptorIssue{
		{Loc: evidence.Location{Path: "z.pb.go", Line: 9}, Detail: "unsupported"},
		{Loc: evidence.Location{Path: "a.pb.go", Line: 2}, Detail: "malformed", Malformed: true},
	}
	input.Configs[0].CustomStatePrefixes = evidence.PrefixList{Present: true, Values: []evidence.Prefix{literalPrefix(15), literalPrefix(1)}, Loc: testLocation()}
	first := Evaluate(input)
	second := Evaluate(input)
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("repeated evaluation differs:\nfirst=%#v\nsecond=%#v", first, second)
	}
	for index := 1; index < len(first); index++ {
		before, after := first[index-1], first[index]
		if before.RuleID > after.RuleID || (before.RuleID == after.RuleID && before.Code > after.Code) {
			t.Fatalf("findings not sorted at %d: %s/%s before %s/%s", index, before.RuleID, before.Code, after.RuleID, after.Code)
		}
	}
}

func TestCNPY003ProtectedAccount(t *testing.T) {
	tests := []struct {
		name   string
		fields []evidence.ProtoField
		want   diagnostic.Decision
		code   string
	}{
		{name: "singular uint64 field seven passes", fields: []evidence.ProtoField{{Name: "nonce", Number: 7, Label: 1, Kind: 4}}, want: diagnostic.DecisionPass},
		{name: "incompatible type blocks", fields: []evidence.ProtoField{{Name: "nonce", Number: 7, Label: 1, Kind: 9}}, want: diagnostic.DecisionBlock, code: "CNPY003-NONCE-CONFLICT"},
		{name: "repeated cardinality blocks", fields: []evidence.ProtoField{{Name: "nonce", Number: 7, Label: 3, Kind: 4}}, want: diagnostic.DecisionBlock, code: "CNPY003-NONCE-CONFLICT"},
		{name: "field seven omission reviews", fields: nil, want: diagnostic.DecisionReview, code: "CNPY003-NONCE-REVIEW"},
		{name: "field ten is not Account nonce", fields: []evidence.ProtoField{{Name: "nonce", Number: 10, Label: 1, Kind: 4}}, want: diagnostic.DecisionReview, code: "CNPY003-NONCE-REVIEW"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			input := oneConfig(nil, nil)
			input.MessageSchemas = map[string][]evidence.MessageSchema{
				"types.Account": {{Name: "types.Account", Fields: test.fields, Loc: evidence.Location{Path: "account.pb.go", Line: 3}}},
			}
			findings := Evaluate(input)
			if got := decision(t, findings); got != test.want {
				t.Fatalf("decision = %q, want %q; findings=%#v", got, test.want, findings)
			}
			var cnpy003 []string
			for _, finding := range findings {
				if finding.RuleID == "CNPY003" {
					cnpy003 = append(cnpy003, finding.Code)
				}
			}
			if test.code == "" && len(cnpy003) != 0 {
				t.Fatalf("unexpected CNPY003 findings: %v", cnpy003)
			}
			if test.code != "" && !reflect.DeepEqual(cnpy003, []string{test.code}) {
				t.Fatalf("CNPY003 codes = %v, want %q", cnpy003, test.code)
			}
		})
	}
}

func TestCNPY003UsesFullyQualifiedAccountIdentity(t *testing.T) {
	input := oneConfig(nil, nil)
	input.MessageSchemas = map[string][]evidence.MessageSchema{
		"other.Account": {{Name: "other.Account", Fields: []evidence.ProtoField{{Name: "nonce", Number: 7, Label: 3, Kind: 9}}, Loc: evidence.Location{Path: "other.pb.go", Line: 3}}},
	}
	findings := Evaluate(input)
	if got := decision(t, findings); got != diagnostic.DecisionPass {
		t.Fatalf("unrelated Account affected decision: %q; findings=%#v", got, findings)
	}
}

func TestCNPY003DuplicateAccountDescriptorsReview(t *testing.T) {
	input := oneConfig(nil, nil)
	input.MessageSchemas = map[string][]evidence.MessageSchema{
		"types.Account": {
			{Name: "types.Account", Loc: evidence.Location{Path: "a.pb.go", Line: 3}},
			{Name: "types.Account", Loc: evidence.Location{Path: "b.pb.go", Line: 3}},
		},
	}
	findings := Evaluate(input)
	if got := decision(t, findings); got != diagnostic.DecisionReview {
		t.Fatalf("decision = %q, want review; findings=%#v", got, findings)
	}
	if len(findings) != 1 || findings[0].Code != "CNPY003-ACCOUNT-UNKNOWN" || len(findings[0].EvidenceLocations) != 2 {
		t.Fatalf("unexpected findings: %#v", findings)
	}
}

func TestCNPY003UnresolvableDescriptorWithoutProvenanceDoesNotBlock(t *testing.T) {
	input := oneConfig([]string{"send"}, []string{"types.Account"})
	input.DescriptorIssues = []evidence.DescriptorIssue{{
		Loc: evidence.Location{Path: "account.pb.go", Line: 4}, Detail: "raw descriptor is not inspectable", Subject: "Account",
	}}
	findings := Evaluate(input)
	if got := decision(t, findings); got != diagnostic.DecisionReview {
		t.Fatalf("decision = %q, want review; findings=%#v", got, findings)
	}
	for _, finding := range findings {
		if finding.Decision == diagnostic.DecisionBlock || finding.Code == "CNPY003-INVALID-DESCRIPTOR" {
			t.Fatalf("unproven descriptor issue was overstated: %#v", finding)
		}
	}
}

func TestCNPY003UsesOnlyProvenSelectedSchemas(t *testing.T) {
	input := oneConfig(nil, nil)
	input.Collected = true
	input.SchemaSelection = evidence.DescriptorSelection{Attempted: true, Proven: true, Loc: evidence.Location{Path: "descriptor_init.go", Line: 18}}
	input.MessageSchemas = map[string][]evidence.MessageSchema{
		"types.Account": {{Name: "types.Account", Fields: []evidence.ProtoField{{Name: "nonce", Number: 7, Label: 1, Kind: 9}}, Loc: evidence.Location{Path: "stale.pb.go", Line: 5}}},
	}
	input.SelectedSchemas = map[string][]evidence.MessageSchema{
		"types.Account": {{Name: "types.Account", Fields: []evidence.ProtoField{{Name: "nonce", Number: 7, Label: 1, Kind: 4}}, Loc: evidence.Location{Path: "account.pb.go", Line: 5}}},
	}
	if findings := Evaluate(input); decision(t, findings) != diagnostic.DecisionPass {
		t.Fatalf("unselected stale schema affected result: %#v", findings)
	}
}

func TestCNPY003SelectionOutcomes(t *testing.T) {
	tests := []struct {
		name      string
		selection evidence.DescriptorSelection
		selected  map[string][]evidence.MessageSchema
		want      diagnostic.Decision
		code      string
	}{
		{
			name:      "selected unrelated only reviews",
			selection: evidence.DescriptorSelection{Attempted: true, Proven: true, Loc: evidence.Location{Path: "descriptor_init.go", Line: 18}},
			selected:  map[string][]evidence.MessageSchema{"other.Account": {{Name: "other.Account", Loc: evidence.Location{Path: "other.pb.go", Line: 5}}}},
			want:      diagnostic.DecisionReview, code: "CNPY003-ACCOUNT-UNKNOWN",
		},
		{
			name:      "unsupported assignment reviews",
			selection: evidence.DescriptorSelection{Attempted: true, Proven: false, Loc: evidence.Location{Path: "descriptor_init.go", Line: 8}, Issues: []evidence.DescriptorIssue{{Loc: evidence.Location{Path: "descriptor_init.go", Line: 8}, Detail: "unsupported"}}},
			want:      diagnostic.DecisionReview, code: "CNPY003-ACCOUNT-UNKNOWN",
		},
		{
			name:      "selected malformed blocks",
			selection: evidence.DescriptorSelection{Attempted: true, Proven: true, Loc: evidence.Location{Path: "descriptor_init.go", Line: 18}, Issues: []evidence.DescriptorIssue{{Loc: evidence.Location{Path: "payload.pb.go", Line: 5}, Detail: "selected descriptor protobuf wire data is malformed", Malformed: true}}},
			want:      diagnostic.DecisionBlock, code: "CNPY003-INVALID-DESCRIPTOR",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			input := oneConfig(nil, nil)
			input.Collected = true
			input.SchemaSelection = test.selection
			input.SelectedSchemas = test.selected
			findings := Evaluate(input)
			if got := decision(t, findings); got != test.want {
				t.Fatalf("decision = %q, want %q; findings=%#v", got, test.want, findings)
			}
			found := false
			for _, finding := range findings {
				found = found || finding.Code == test.code
			}
			if !found {
				t.Fatalf("finding %q missing: %#v", test.code, findings)
			}
		})
	}
}

func oneConfig(names, urls []string) evidence.Result {
	return evidence.Result{
		SourceFileCount: 1,
		Messages:        map[string]evidence.Location{},
		Configs: []evidence.Config{{
			Loc:                   evidence.Location{Path: "config.go", Line: 10},
			CanopyType:            true,
			SupportedTransactions: evidence.StringList{Present: true, Values: names, Loc: evidence.Location{Path: "config.go", Line: 11}},
			TransactionTypeURLs:   evidence.StringList{Present: true, Values: urls, Loc: evidence.Location{Path: "config.go", Line: 12}},
			CustomStatePrefixes:   evidence.PrefixList{Present: true, Loc: evidence.Location{Path: "config.go", Line: 13}},
		}},
	}
}

func registry(names, urls []string, descriptorCount int, messageNames ...string) evidence.Result {
	result := oneConfig(names, urls)
	result.DescriptorCount = descriptorCount
	for _, name := range messageNames {
		result.Messages[name] = evidence.Location{Path: "demo.pb.go", Line: 3}
	}
	return result
}

func withDescriptorIssue(input evidence.Result, malformed bool) evidence.Result {
	input.DescriptorIssues = []evidence.DescriptorIssue{{Loc: evidence.Location{Path: "demo.pb.go", Line: 3}, Detail: "descriptor fixture", Malformed: malformed}}
	return input
}

func dynamicRegistry() evidence.Result {
	result := oneConfig(nil, nil)
	result.Configs[0].SupportedTransactions.Dynamic = true
	return result
}

func dynamicPrefixes() evidence.Result {
	result := oneConfig(nil, nil)
	result.Configs[0].CustomStatePrefixes.Dynamic = true
	return result
}

func mutatedConfig() evidence.Result {
	result := oneConfig([]string{"send", "reward"}, []string{"demo.MessageSend"})
	result.Configs[0].Mutated = true
	return result
}

func literalPrefix(values ...byte) evidence.Prefix {
	return evidence.Prefix{Bytes: values, Loc: testLocation()}
}

func testLocation() evidence.Location {
	return evidence.Location{Path: "config.go", Line: 13}
}

func decision(t *testing.T, findings []diagnostic.Finding) diagnostic.Decision {
	t.Helper()
	value, err := diagnostic.DecisionFor(findings)
	if err != nil {
		t.Fatalf("DecisionFor() error = %v", err)
	}
	return value
}

func findingCodes(findings []diagnostic.Finding) []string {
	result := make([]string, len(findings))
	for index := range findings {
		result[index] = findings[index].Code
	}
	return result
}
