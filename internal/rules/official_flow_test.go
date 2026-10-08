package rules

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jerrygeorge360/canopy-preflight/internal/diagnostic"
	"github.com/jerrygeorge360/canopy-preflight/internal/evidence"
)

func TestOfficialPointerFlowKeepsBlockingChecksAuthoritative(t *testing.T) {
	tests := []struct {
		name   string
		old    string
		new    string
		ruleID string
		code   string
	}{
		{
			name:   "transaction count mismatch",
			old:    "SupportedTransactions: []string{\"send\"}",
			new:    "SupportedTransactions: []string{\"send\", \"reward\"}",
			ruleID: "CNPY001",
			code:   "CNPY001-COUNT",
		},
		{
			name:   "reserved prefix",
			old:    "CustomStatePrefixes:   [][]byte{{100}}",
			new:    "CustomStatePrefixes:   [][]byte{{7}}",
			ruleID: "CNPY002",
			code:   "CNPY002-DECLARED-COLLISION",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := copyOfficialRulesFixture(t, "official-pointer-valid")
			replaceOfficialRulesFixtureText(t, root, "config.go", test.old, test.new)
			input, err := evidence.Collect(root)
			if err != nil {
				t.Fatalf("Collect() error = %v", err)
			}
			if len(input.Configs) != 1 || input.Configs[0].Mutated {
				t.Fatalf("official flow was not proven immutable: %#v", input.Configs)
			}
			findings := Evaluate(input)
			finding, ok := officialFinding(findings, test.ruleID, test.code)
			if !ok || finding.Decision != diagnostic.DecisionBlock {
				t.Fatalf("blocking finding %s missing: %#v", test.code, findings)
			}
			if _, ok := officialFinding(findings, test.ruleID, test.ruleID+"-CONFIG-REVIEW"); ok {
				t.Fatalf("proven flow was downgraded to config review: %#v", findings)
			}
		})
	}
}

func TestCNPY001UsesOnlyProvenSelectedDescriptorsForOfficialFlow(t *testing.T) {
	tests := []struct {
		name          string
		fixture       string
		wantReview    bool
		wantSelection bool
		wantIssue     bool
	}{
		{name: "valid selected registry", fixture: "official-pointer-valid", wantSelection: true},
		{name: "unproven selection", fixture: "official-pointer-unproven", wantReview: true},
		{name: "issue-bearing selection", fixture: "official-pointer-issue", wantReview: true, wantSelection: true, wantIssue: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			input, err := evidence.Collect(officialRulesFixture(test.fixture))
			if err != nil {
				t.Fatalf("Collect() error = %v", err)
			}
			if len(input.Configs) != 1 || input.Configs[0].Mutated {
				t.Fatalf("official flow was not proven immutable: %#v", input.Configs)
			}
			if input.SchemaSelection.Proven != test.wantSelection {
				t.Fatalf("selection proven = %v, want %v; selection=%#v", input.SchemaSelection.Proven, test.wantSelection, input.SchemaSelection)
			}
			if (len(input.SchemaSelection.Issues) > 0) != test.wantIssue {
				t.Fatalf("selection issues = %#v, want issue=%v", input.SchemaSelection.Issues, test.wantIssue)
			}
			findings := Evaluate(input)
			reviews := 0
			for _, finding := range findings {
				if finding.RuleID == "CNPY001" && finding.Code == "CNPY001-DESCRIPTOR-REVIEW" {
					reviews++
				}
			}
			if test.wantReview && reviews == 0 {
				t.Fatalf("unproven or issue-bearing selection did not require registry review: %#v", findings)
			}
			if !test.wantReview && reviews != 0 {
				t.Fatalf("proven selected registry produced provenance review: %#v", findings)
			}
		})
	}
}

func officialRulesFixture(name string) string {
	return filepath.Join("..", "..", "testdata", "phase2", name)
}

func copyOfficialRulesFixture(t *testing.T, name string) string {
	t.Helper()
	source := officialRulesFixture(name)
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

func replaceOfficialRulesFixtureText(t *testing.T, root, name, old, replacement string) {
	t.Helper()
	path := filepath.Join(root, name)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	if !strings.Contains(string(data), old) {
		t.Fatalf("%s does not contain mutation target %q", name, old)
	}
	updated := strings.Replace(string(data), old, replacement, 1)
	if err := os.WriteFile(path, []byte(updated), 0o600); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

func officialFinding(findings []diagnostic.Finding, ruleID, code string) (diagnostic.Finding, bool) {
	for _, finding := range findings {
		if finding.RuleID == ruleID && finding.Code == code {
			return finding, true
		}
	}
	return diagnostic.Finding{}, false
}
