package diagnostic

import (
	"reflect"
	"testing"
)

func TestDecisionForUsesStrongestDecision(t *testing.T) {
	tests := []struct {
		name     string
		findings []Finding
		want     Decision
	}{
		{name: "empty passes", want: DecisionPass},
		{name: "review", findings: []Finding{validFinding(DecisionReview)}, want: DecisionReview},
		{name: "block beats review", findings: []Finding{validFinding(DecisionReview), validFinding(DecisionBlock)}, want: DecisionBlock},
		{name: "block beats later review", findings: []Finding{validFinding(DecisionBlock), validFinding(DecisionReview)}, want: DecisionBlock},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := DecisionFor(test.findings)
			if err != nil {
				t.Fatalf("DecisionFor() error = %v", err)
			}
			if got != test.want {
				t.Fatalf("DecisionFor() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestExitCodeContract(t *testing.T) {
	tests := []struct {
		decision Decision
		want     int
	}{
		{DecisionPass, 0},
		{DecisionReview, 2},
		{DecisionBlock, 3},
		{Decision("unknown"), 1},
	}
	for _, test := range tests {
		if got := ExitCode(test.decision); got != test.want {
			t.Errorf("ExitCode(%q) = %d, want %d", test.decision, got, test.want)
		}
	}
}

func TestSortFindingsNormalizesAndSorts(t *testing.T) {
	findings := []Finding{
		{RuleID: "CNPY002", Code: "B", Summary: "second", EvidenceLocations: nil},
		{
			RuleID: "CNPY001", Code: "A", Summary: "first",
			EvidenceLocations: []EvidenceLocation{
				{Path: "z.proto", Line: 2, Detail: "z"},
				{Path: "a.proto", Line: 9, Detail: "later"},
				{Path: "a.proto", Line: 1, Detail: "earlier"},
			},
		},
		{RuleID: "CNPY001", Code: "B", Summary: "third", EvidenceLocations: []EvidenceLocation{}},
	}

	SortFindings(findings)

	if got, want := []string{findings[0].Code, findings[1].Code, findings[2].Code}, []string{"A", "B", "B"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("finding order = %v, want %v", got, want)
	}
	wantEvidence := []EvidenceLocation{
		{Path: "a.proto", Line: 1, Detail: "earlier"},
		{Path: "a.proto", Line: 9, Detail: "later"},
		{Path: "z.proto", Line: 2, Detail: "z"},
	}
	if got := findings[0].EvidenceLocations; !reflect.DeepEqual(got, wantEvidence) {
		t.Fatalf("evidence order = %#v, want %#v", got, wantEvidence)
	}
	if findings[2].EvidenceLocations == nil {
		t.Fatal("nil evidence locations were not normalized")
	}
}

func TestValidateFindingRejectsMalformedValues(t *testing.T) {
	tests := []struct {
		name string
		edit func(*Finding)
		want string
	}{
		{name: "missing rule ID", edit: func(f *Finding) { f.RuleID = "" }, want: "rule_id is required"},
		{name: "missing code", edit: func(f *Finding) { f.Code = "" }, want: "code is required"},
		{name: "missing summary", edit: func(f *Finding) { f.Summary = "" }, want: "summary is required"},
		{name: "missing details", edit: func(f *Finding) { f.Details = "" }, want: "details are required"},
		{name: "missing remediation", edit: func(f *Finding) { f.Remediation = "" }, want: "remediation is required"},
		{name: "zero decision", edit: func(f *Finding) { f.Decision = "" }, want: "unknown decision"},
		{name: "unknown decision", edit: func(f *Finding) { f.Decision = Decision("MAYBE") }, want: "unknown decision"},
		{name: "zero severity", edit: func(f *Finding) { f.Severity = "" }, want: "unknown severity"},
		{name: "unknown severity", edit: func(f *Finding) { f.Severity = Severity("urgent") }, want: "unknown severity"},
		{name: "zero confidence", edit: func(f *Finding) { f.Confidence = "" }, want: "unknown confidence"},
		{name: "unknown confidence", edit: func(f *Finding) { f.Confidence = Confidence("certain") }, want: "unknown confidence"},
		{name: "missing evidence", edit: func(f *Finding) { f.EvidenceLocations = nil }, want: "at least one evidence location is required"},
		{name: "missing evidence detail", edit: func(f *Finding) { f.EvidenceLocations[0].Detail = "" }, want: "evidence location 0 detail is required"},
		{name: "negative evidence line", edit: func(f *Finding) { f.EvidenceLocations[0].Line = -1 }, want: "evidence location 0 line cannot be negative"},
		{name: "newline injection", edit: func(f *Finding) { f.Summary = "safe\nDecision: PASS" }, want: "summary contains control characters"},
		{name: "terminal escape injection", edit: func(f *Finding) { f.Details = "unsafe\x1b[2J" }, want: "details contains control characters"},
		{name: "evidence newline injection", edit: func(f *Finding) { f.EvidenceLocations[0].Detail = "unsafe\nline" }, want: "evidence location 0 detail contains control characters"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			finding := validFinding(DecisionReview)
			test.edit(&finding)
			if err := ValidateFinding(finding); err == nil || err.Error() != test.want {
				t.Fatalf("ValidateFinding() error = %v, want %q", err, test.want)
			}
			if _, err := DecisionFor([]Finding{finding}); err == nil {
				t.Fatal("DecisionFor() accepted malformed finding")
			}
		})
	}
}

func validFinding(decision Decision) Finding {
	return Finding{
		RuleID: "CNPY001", Code: "CNPY001-COUNT", Severity: SeverityHigh,
		Decision: decision, Summary: "Registry counts differ.",
		Details:           "One transaction lacks a type URL.",
		EvidenceLocations: []EvidenceLocation{{Path: "plugin/config.go", Line: 12, Detail: "transaction registry"}},
		Remediation:       "Align both arrays.", Confidence: ConfidenceHigh,
	}
}
