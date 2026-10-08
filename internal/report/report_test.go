package report

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/jerrygeorge360/canopy-preflight/internal/diagnostic"
)

func TestJSONGoldenAndContract(t *testing.T) {
	value := fixtureReport(t)
	var output bytes.Buffer
	if err := WriteJSON(&output, value); err != nil {
		t.Fatalf("WriteJSON() error = %v", err)
	}
	assertGolden(t, "json.golden", output.String())
	if !strings.HasSuffix(output.String(), "\n") {
		t.Fatal("JSON report must end in a newline")
	}
	if strings.Contains(output.String(), `"findings": null`) || strings.Contains(output.String(), `"evidence_locations": null`) {
		t.Fatal("JSON arrays must never be null")
	}
}

func TestEmptyJSONUsesNonNullFindings(t *testing.T) {
	var output bytes.Buffer
	value, err := New("dev", "project", nil)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if err := WriteJSON(&output, value); err != nil {
		t.Fatalf("WriteJSON() error = %v", err)
	}
	if !strings.Contains(output.String(), `"findings": []`) {
		t.Fatalf("empty findings encoded unexpectedly:\n%s", output.String())
	}
}

func TestHumanGoldenAndDecisionMatchesJSON(t *testing.T) {
	value := fixtureReport(t)
	var human bytes.Buffer
	if err := WriteHuman(&human, value); err != nil {
		t.Fatalf("WriteHuman() error = %v", err)
	}
	assertGolden(t, "human.golden", human.String())
	if !strings.Contains(human.String(), "Decision: "+string(value.Decision)) {
		t.Fatalf("human decision does not match normalized report: %q", value.Decision)
	}
}

func TestNewCopiesInputBeforeNormalization(t *testing.T) {
	input := []diagnostic.Finding{{
		RuleID: "CNPY001", Code: "CNPY001-COUNT", Severity: diagnostic.SeverityHigh,
		Decision: diagnostic.DecisionBlock, Summary: "Registry counts differ.",
		Details: "One transaction lacks a type URL.", Remediation: "Align both arrays.", Confidence: diagnostic.ConfidenceHigh,
		EvidenceLocations: []diagnostic.EvidenceLocation{
			{Path: "z.proto", Line: 2, Detail: "later evidence"},
			{Path: "a.proto", Line: 1, Detail: "earlier evidence"},
		},
	}}
	want := append([]diagnostic.EvidenceLocation(nil), input[0].EvidenceLocations...)
	if _, err := New("dev", "project", input); err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if !reflect.DeepEqual(input[0].EvidenceLocations, want) {
		t.Fatalf("New mutated caller evidence: got %#v, want %#v", input[0].EvidenceLocations, want)
	}
}

func TestWritersRejectUnsafePaths(t *testing.T) {
	tests := []struct {
		name     string
		target   string
		evidence string
	}{
		{name: "Unix absolute evidence", target: "fixture", evidence: "/etc/passwd"},
		{name: "Windows drive evidence", target: "fixture", evidence: "C:/secret/file.proto"},
		{name: "UNC backslash evidence", target: "fixture", evidence: `\\server\share\file.proto`},
		{name: "parent traversal evidence", target: "fixture", evidence: "../secret.proto"},
		{name: "absolute target", target: "/tmp/project", evidence: "plugin/config.go"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			value := rawReport(test.target, test.evidence)
			for _, writer := range []struct {
				name  string
				write func(*bytes.Buffer, Report) error
			}{
				{name: "human", write: func(w *bytes.Buffer, r Report) error { return WriteHuman(w, r) }},
				{name: "json", write: func(w *bytes.Buffer, r Report) error { return WriteJSON(w, r) }},
			} {
				t.Run(writer.name, func(t *testing.T) {
					var output bytes.Buffer
					if err := writer.write(&output, value); err == nil {
						t.Fatalf("writer accepted target=%q evidence=%q", test.target, test.evidence)
					}
					if output.Len() != 0 {
						t.Fatalf("writer emitted partial output: %q", output.String())
					}
				})
			}
		})
	}
}

func fixtureReport(t *testing.T) Report {
	t.Helper()
	value, err := New("v0.1.0", "fixture", []diagnostic.Finding{
		{
			RuleID: "CNPY002", Code: "CNPY002-PREFIX-REVIEW", Severity: diagnostic.SeverityMedium,
			Decision: diagnostic.DecisionReview, Summary: "Prefix needs review.",
			Details: "The declaration is dynamic.", Remediation: "Use a literal prefix.", Confidence: diagnostic.ConfidenceMedium,
			EvidenceLocations: []diagnostic.EvidenceLocation{{Path: "plugin/prefix.go", Line: 7, Detail: "dynamic prefix declaration"}},
		},
		{
			RuleID: "CNPY001", Code: "CNPY001-COUNT", Severity: diagnostic.SeverityHigh,
			Decision: diagnostic.DecisionBlock, Summary: "Registry counts differ.",
			Details: "One transaction lacks a type URL.", Remediation: "Align both arrays.", Confidence: diagnostic.ConfidenceHigh,
			EvidenceLocations: []diagnostic.EvidenceLocation{
				{Path: "plugin/config.go", Line: 12, Detail: "supported transactions"},
				{Path: "plugin/config.go", Line: 18, Detail: "type URLs"},
			},
		},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return value
}

func rawReport(target, evidence string) Report {
	return Report{
		SchemaVersion: SchemaVersion,
		Tool:          Tool{Name: "canopy-doctor", Version: "test"},
		Target:        target,
		Findings: []diagnostic.Finding{{
			RuleID: "CNPY001", Code: "CNPY001-COUNT", Severity: diagnostic.SeverityHigh,
			Decision: diagnostic.DecisionBlock, Summary: "Registry counts differ.",
			Details: "One transaction lacks a type URL.", Remediation: "Align both arrays.", Confidence: diagnostic.ConfidenceHigh,
			EvidenceLocations: []diagnostic.EvidenceLocation{{Path: evidence, Line: 1, Detail: "transaction registry"}},
		}},
	}
}

func assertGolden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("..", "..", "testdata", "phase1", name)
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden %s: %v", path, err)
	}
	if got != string(want) {
		t.Fatalf("output differs from %s\n--- got ---\n%s--- want ---\n%s", name, got, want)
	}
}
