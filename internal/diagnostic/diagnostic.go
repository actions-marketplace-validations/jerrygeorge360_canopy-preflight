// Package diagnostic defines the stable findings and release decisions shared
// by rule evaluation and report rendering.
package diagnostic

import (
	"fmt"
	"sort"
	"strings"
	"unicode"
)

// Decision is the release outcome produced by deterministic checks.
type Decision string

const (
	DecisionPass   Decision = "PASS"
	DecisionReview Decision = "REVIEW REQUIRED"
	DecisionBlock  Decision = "DO NOT RELEASE"
)

// Severity describes the importance of a finding independently of its release
// decision.
type Severity string

const (
	SeverityInfo     Severity = "info"
	SeverityLow      Severity = "low"
	SeverityMedium   Severity = "medium"
	SeverityHigh     Severity = "high"
	SeverityCritical Severity = "critical"
)

// Confidence records how directly the available evidence supports a finding.
type Confidence string

const (
	ConfidenceLow    Confidence = "low"
	ConfidenceMedium Confidence = "medium"
	ConfidenceHigh   Confidence = "high"
)

// EvidenceLocation points to evidence using a project-relative path. Line is
// one-based when known and zero when a finding applies to the whole file.
type EvidenceLocation struct {
	Path   string `json:"path"`
	Line   int    `json:"line"`
	Detail string `json:"detail"`
}

// Finding is the normalized output of a compatibility rule.
type Finding struct {
	RuleID            string             `json:"rule_id"`
	Code              string             `json:"code"`
	Severity          Severity           `json:"severity"`
	Decision          Decision           `json:"decision"`
	Summary           string             `json:"summary"`
	Details           string             `json:"details"`
	EvidenceLocations []EvidenceLocation `json:"evidence_locations"`
	Remediation       string             `json:"remediation"`
	Confidence        Confidence         `json:"confidence"`
}

// SortFindings orders findings by their public output contract. It also
// normalizes a nil evidence list to an empty JSON array.
func SortFindings(findings []Finding) {
	for i := range findings {
		if findings[i].EvidenceLocations == nil {
			findings[i].EvidenceLocations = []EvidenceLocation{}
		}
		sort.SliceStable(findings[i].EvidenceLocations, func(a, b int) bool {
			left := findings[i].EvidenceLocations[a]
			right := findings[i].EvidenceLocations[b]
			if left.Path != right.Path {
				return left.Path < right.Path
			}
			if left.Line != right.Line {
				return left.Line < right.Line
			}
			return left.Detail < right.Detail
		})
	}
	sort.SliceStable(findings, func(i, j int) bool {
		a, b := findings[i], findings[j]
		if a.RuleID != b.RuleID {
			return a.RuleID < b.RuleID
		}
		if a.Code != b.Code {
			return a.Code < b.Code
		}
		ap, al := firstEvidence(a)
		bp, bl := firstEvidence(b)
		if ap != bp {
			return ap < bp
		}
		if al != bl {
			return al < bl
		}
		return a.Summary < b.Summary
	})
}

func firstEvidence(f Finding) (string, int) {
	if len(f.EvidenceLocations) == 0 {
		return "", 0
	}
	return f.EvidenceLocations[0].Path, f.EvidenceLocations[0].Line
}

// ValidateFinding rejects incomplete findings and unknown enum values. A
// malformed finding is an operational error, never evidence for PASS.
func ValidateFinding(finding Finding) error {
	if strings.TrimSpace(finding.RuleID) == "" {
		return fmt.Errorf("rule_id is required")
	}
	if strings.TrimSpace(finding.Code) == "" {
		return fmt.Errorf("code is required")
	}
	if strings.TrimSpace(finding.Summary) == "" {
		return fmt.Errorf("summary is required")
	}
	if strings.TrimSpace(finding.Details) == "" {
		return fmt.Errorf("details are required")
	}
	if strings.TrimSpace(finding.Remediation) == "" {
		return fmt.Errorf("remediation is required")
	}
	textFields := []struct{ name, value string }{
		{"rule_id", finding.RuleID},
		{"code", finding.Code},
		{"summary", finding.Summary},
		{"details", finding.Details},
		{"remediation", finding.Remediation},
	}
	for _, field := range textFields {
		if strings.IndexFunc(field.value, unicode.IsControl) >= 0 {
			return fmt.Errorf("%s contains control characters", field.name)
		}
	}
	switch finding.Decision {
	case DecisionPass, DecisionReview, DecisionBlock:
	default:
		return fmt.Errorf("unknown decision")
	}
	switch finding.Severity {
	case SeverityInfo, SeverityLow, SeverityMedium, SeverityHigh, SeverityCritical:
	default:
		return fmt.Errorf("unknown severity")
	}
	switch finding.Confidence {
	case ConfidenceLow, ConfidenceMedium, ConfidenceHigh:
	default:
		return fmt.Errorf("unknown confidence")
	}
	if len(finding.EvidenceLocations) == 0 {
		return fmt.Errorf("at least one evidence location is required")
	}
	for i, evidence := range finding.EvidenceLocations {
		if strings.TrimSpace(evidence.Detail) == "" {
			return fmt.Errorf("evidence location %d detail is required", i)
		}
		if evidence.Line < 0 {
			return fmt.Errorf("evidence location %d line cannot be negative", i)
		}
		if strings.IndexFunc(evidence.Detail, unicode.IsControl) >= 0 {
			return fmt.Errorf("evidence location %d detail contains control characters", i)
		}
	}
	return nil
}

// DecisionFor returns the strongest decision in findings. Blocking findings
// take precedence over review findings, and an empty set passes. It validates
// every finding before aggregation so implementation defects cannot become a
// successful result.
func DecisionFor(findings []Finding) (Decision, error) {
	decision := DecisionPass
	for i, finding := range findings {
		if err := ValidateFinding(finding); err != nil {
			return "", fmt.Errorf("finding %d: %w", i, err)
		}
		switch finding.Decision {
		case DecisionBlock:
			decision = DecisionBlock
		case DecisionReview:
			if decision != DecisionBlock {
				decision = DecisionReview
			}
		}
	}
	return decision, nil
}

// ExitCode maps a release decision to the stable CLI exit contract.
func ExitCode(decision Decision) int {
	switch decision {
	case DecisionPass:
		return 0
	case DecisionReview:
		return 2
	case DecisionBlock:
		return 3
	default:
		return 1
	}
}
