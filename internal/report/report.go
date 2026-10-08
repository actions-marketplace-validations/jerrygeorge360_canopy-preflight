// Package report renders deterministic human and JSON compatibility reports.
package report

import (
	"encoding/json"
	"fmt"
	"io"
	"path"
	"strings"
	"unicode"

	"github.com/jerrygeorge360/canopy-preflight/internal/diagnostic"
)

const SchemaVersion = "1.0"

// Tool identifies the producer of a report.
type Tool struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// Report is the versioned public output contract.
type Report struct {
	SchemaVersion string               `json:"schema_version"`
	Tool          Tool                 `json:"tool"`
	Target        string               `json:"target"`
	Decision      diagnostic.Decision  `json:"decision"`
	Findings      []diagnostic.Finding `json:"findings"`
}

// New creates and validates a normalized report, copying findings before
// sorting them.
func New(toolVersion, target string, findings []diagnostic.Finding) (Report, error) {
	return normalize(Report{
		SchemaVersion: SchemaVersion,
		Tool:          Tool{Name: "canopy-doctor", Version: toolVersion},
		Target:        target,
		Findings:      findings,
	})
}

func normalize(value Report) (Report, error) {
	if value.SchemaVersion != SchemaVersion {
		return Report{}, fmt.Errorf("unsupported schema version")
	}
	if value.Tool.Name != "canopy-doctor" || strings.TrimSpace(value.Tool.Version) == "" || hasControl(value.Tool.Version) {
		return Report{}, fmt.Errorf("invalid tool identity")
	}
	if err := validateReportPath(value.Target); err != nil {
		return Report{}, fmt.Errorf("invalid target path")
	}
	findings := value.Findings
	owned := make([]diagnostic.Finding, len(findings))
	copy(owned, findings)
	for i := range owned {
		owned[i].EvidenceLocations = append([]diagnostic.EvidenceLocation(nil), findings[i].EvidenceLocations...)
		if err := diagnostic.ValidateFinding(owned[i]); err != nil {
			return Report{}, fmt.Errorf("finding %d: %w", i, err)
		}
		for j := range owned[i].EvidenceLocations {
			if err := validateReportPath(owned[i].EvidenceLocations[j].Path); err != nil {
				return Report{}, fmt.Errorf("finding %d evidence location %d has an invalid path", i, j)
			}
		}
	}
	if owned == nil {
		owned = []diagnostic.Finding{}
	}
	diagnostic.SortFindings(owned)
	decision, err := diagnostic.DecisionFor(owned)
	if err != nil {
		return Report{}, err
	}
	value.Decision = decision
	value.Findings = owned
	return value, nil
}

func validateReportPath(value string) error {
	if value == "" || strings.Contains(value, "\\") || strings.HasPrefix(value, "/") || hasControl(value) {
		return fmt.Errorf("path must be a non-empty slash-separated relative path")
	}
	if len(value) >= 2 && ((value[0] >= 'A' && value[0] <= 'Z') || (value[0] >= 'a' && value[0] <= 'z')) && value[1] == ':' {
		return fmt.Errorf("Windows drive paths are not allowed")
	}
	clean := path.Clean(value)
	if clean != value || clean == ".." || strings.HasPrefix(clean, "../") || path.IsAbs(clean) {
		return fmt.Errorf("path must be normalized and cannot escape the project")
	}
	return nil
}

func hasControl(value string) bool {
	return strings.IndexFunc(value, unicode.IsControl) >= 0
}

// WriteJSON writes one indented JSON document followed by a newline. Struct
// field order defines the stable top-level and finding field order.
func WriteJSON(w io.Writer, value Report) error {
	value, err := normalize(value)
	if err != nil {
		return err
	}
	encoder := json.NewEncoder(w)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}

// WriteHuman writes a deterministic terminal report.
func WriteHuman(w io.Writer, value Report) error {
	value, err := normalize(value)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w, "Canopy Doctor"); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "Target: %s\nDecision: %s\n", value.Target, value.Decision); err != nil {
		return err
	}
	if len(value.Findings) == 0 {
		_, err := fmt.Fprintln(w, "Findings: 0")
		return err
	}
	if _, err := fmt.Fprintf(w, "Findings: %d\n", len(value.Findings)); err != nil {
		return err
	}
	for _, finding := range value.Findings {
		if _, err := fmt.Fprintf(w, "\n[%s] %s %s\n%s\n", finding.Decision, finding.RuleID, finding.Code, finding.Summary); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(w, "Severity: %s | Confidence: %s\n", finding.Severity, finding.Confidence); err != nil {
			return err
		}
		if finding.Details != "" {
			if _, err := fmt.Fprintf(w, "Details: %s\n", finding.Details); err != nil {
				return err
			}
		}
		for _, evidence := range finding.EvidenceLocations {
			location := evidence.Path
			if evidence.Line > 0 {
				location = fmt.Sprintf("%s:%d", location, evidence.Line)
			}
			if _, err := fmt.Fprintf(w, "Evidence: %s - %s\n", location, evidence.Detail); err != nil {
				return err
			}
		}
		if finding.Remediation != "" {
			if _, err := fmt.Fprintf(w, "Remediation: %s\n", finding.Remediation); err != nil {
				return err
			}
		}
	}
	return nil
}
