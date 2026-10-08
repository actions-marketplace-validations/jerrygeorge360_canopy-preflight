package rules

import (
	"reflect"
	"strings"
	"testing"

	"github.com/jerrygeorge360/canopy-preflight/internal/diagnostic"
	"github.com/jerrygeorge360/canopy-preflight/internal/upstream"
)

const (
	testBaseSHA      = "1111111111111111111111111111111111111111"
	testTargetSHA    = "2222222222222222222222222222222222222222"
	testCandidateSHA = "3333333333333333333333333333333333333333"
)

func TestEvaluateForkDriftActivationBoundary(t *testing.T) {
	input := forkSnapshot(1)
	for _, test := range []struct {
		name       string
		deployment uint64
		want       diagnostic.Decision
		code       string
	}{
		{name: "before", deployment: 99, want: diagnostic.DecisionPass, code: "CNPY004-CLEAN"},
		{name: "at", deployment: 100, want: diagnostic.DecisionBlock, code: "CNPY004-ACTIVE-VERSION"},
		{name: "after", deployment: 101, want: diagnostic.DecisionBlock, code: "CNPY004-ACTIVE-VERSION"},
	} {
		t.Run(test.name, func(t *testing.T) {
			findings := EvaluateForkDrift(input, ReleaseContext{Supplied: true, DeploymentHeight: test.deployment, ActivationHeight: 100, RequiredProtocolVersion: 2})
			if got, err := diagnostic.DecisionFor(findings); err != nil || got != test.want {
				t.Fatalf("DecisionFor() = %q, %v; want %q", got, err, test.want)
			}
			if !hasFindingCode(findings, test.code) {
				t.Fatalf("findings = %#v, want code %s", findings, test.code)
			}
		})
	}
}

func TestEvaluateForkDriftDecisionPrecedence(t *testing.T) {
	input := forkSnapshot(1)
	input.ChangedSensitivePaths = []string{"lib/plugin.go", "fsm/state.go"}
	findings := EvaluateForkDrift(input, ReleaseContext{Supplied: true, DeploymentHeight: 100, ActivationHeight: 100, RequiredProtocolVersion: 2})
	got, err := diagnostic.DecisionFor(findings)
	if err != nil || got != diagnostic.DecisionBlock {
		t.Fatalf("DecisionFor() = %q, %v; want DO NOT RELEASE", got, err)
	}
	if !hasFindingCode(findings, "CNPY004-ACTIVE-VERSION") || countFindingCode(findings, "CNPY004-SENSITIVE-DRIFT") != 2 {
		t.Fatalf("unexpected findings: %#v", findings)
	}
}

func TestEvaluateForkDriftMissingContextReviews(t *testing.T) {
	findings := EvaluateForkDrift(forkSnapshot(2), ReleaseContext{})
	got, err := diagnostic.DecisionFor(findings)
	if err != nil || got != diagnostic.DecisionReview || !hasFindingCode(findings, "CNPY004-CONTEXT-REVIEW") {
		t.Fatalf("findings = %#v, decision = %q, err = %v", findings, got, err)
	}
}

func TestEvaluateForkDriftSortsPathsAndRecordsAllImmutableEvidence(t *testing.T) {
	input := forkSnapshot(2)
	input.ChangedSensitivePaths = []string{"lib/plugin.go", "fsm/automatic.go", "fsm/state.go"}
	findings := EvaluateForkDrift(input, ReleaseContext{Supplied: true, DeploymentHeight: 9, ActivationHeight: 10, RequiredProtocolVersion: 3})

	var paths []string
	for _, finding := range findings {
		if finding.Code != "CNPY004-SENSITIVE-DRIFT" {
			continue
		}
		paths = append(paths, finding.EvidenceLocations[0].Path)
		for _, sha := range []string{testBaseSHA, testTargetSHA, testCandidateSHA} {
			if !strings.Contains(finding.Details, sha) {
				t.Errorf("%s details omit SHA %s: %q", finding.Code, sha, finding.Details)
			}
		}
	}
	want := []string{"fsm/automatic.go", "fsm/state.go", "lib/plugin.go"}
	if !reflect.DeepEqual(paths, want) {
		t.Fatalf("finding paths = %#v, want %#v", paths, want)
	}
}

func forkSnapshot(version uint64) upstream.Snapshot {
	return upstream.Snapshot{
		BaseSHA: testBaseSHA, TargetSHA: testTargetSHA, CandidateSHA: testCandidateSHA,
		CandidateProtocolVersion: version, ProtocolVersionLine: 3, ChangedSensitivePaths: []string{},
	}
}

func hasFindingCode(findings []diagnostic.Finding, code string) bool {
	return countFindingCode(findings, code) > 0
}

func countFindingCode(findings []diagnostic.Finding, code string) int {
	count := 0
	for _, finding := range findings {
		if finding.Code == code {
			count++
		}
	}
	return count
}
