// Package rules evaluates Canopy compatibility rules from typed source evidence.
package rules

import (
	"fmt"
	"sort"
	"strings"
	"unicode"

	"github.com/jerrygeorge360/canopy-preflight/internal/diagnostic"
	"github.com/jerrygeorge360/canopy-preflight/internal/evidence"
	"github.com/jerrygeorge360/canopy-preflight/internal/upstream"
)

// Evaluate runs the implemented rule set. Findings, rather than evaluation order,
// determine the final release decision.
func Evaluate(input evidence.Result) []diagnostic.Finding {
	var findings []diagnostic.Finding
	findings = append(findings, evaluateProtectedAccount(input)...)
	for _, issue := range input.Issues {
		findings = append(findings, review("CNPY001", "CNPY001-CONFIG-REVIEW", issue.Loc,
			"Plugin configuration cannot be classified safely.", issue.Detail+"; deterministic validation is incomplete.",
			"Rewrite ContractConfig as one package-level direct PluginConfig composite literal."))
	}
	if len(input.Configs) == 0 && len(input.Issues) == 0 {
		findings = append(findings, review("CNPY001", "CNPY001-CONFIG-REVIEW", evidence.Location{Path: "."},
			"Plugin configuration was not found.", "No supported package-level ContractConfig declaration was found.",
			"Declare exactly one package-level ContractConfig as a direct PluginConfig composite literal."))
	}
	if len(input.Configs) > 1 {
		locations := make([]diagnostic.EvidenceLocation, 0, len(input.Configs))
		for _, config := range input.Configs {
			locations = append(locations, loc(config.Loc, "ContractConfig declaration"))
		}
		findings = append(findings, diagnostic.Finding{
			RuleID: "CNPY001", Code: "CNPY001-CONFIG-REVIEW", Severity: diagnostic.SeverityMedium,
			Decision: diagnostic.DecisionReview, Summary: "Multiple plugin configurations were found.",
			Details:           fmt.Sprintf("Found %d supported ContractConfig declarations; the active declaration is ambiguous.", len(input.Configs)),
			EvidenceLocations: locations, Remediation: "Keep exactly one package-level ContractConfig declaration.", Confidence: diagnostic.ConfidenceHigh,
		})
	}
	// Blocking checks require one unambiguous configuration and fully parsed
	// source. Otherwise the only defensible outcome is review.
	if len(input.Configs) != 1 || len(input.Issues) > 0 {
		diagnostic.SortFindings(findings)
		return findings
	}
	if !input.Configs[0].CanopyType {
		findings = append(findings,
			review("CNPY001", "CNPY001-CONFIG-REVIEW", input.Configs[0].Loc,
				"Canopy PluginConfig identity is not proven.", "A matching type name was found without a same-package types.PluginConfig descriptor.",
				"Include the generated Canopy PluginConfig descriptor in the plugin package."),
			review("CNPY002", "CNPY002-PREFIX-REVIEW", input.Configs[0].Loc,
				"Canopy PluginConfig identity is not proven.", "Prefix declarations cannot block release until the configuration type is tied to Canopy.",
				"Include the generated Canopy PluginConfig descriptor in the plugin package."),
		)
		diagnostic.SortFindings(findings)
		return findings
	}
	for _, config := range input.Configs {
		findings = append(findings, evaluateRegistry(config, input)...)
		findings = append(findings, evaluatePrefixes(config)...)
	}
	diagnostic.SortFindings(findings)
	return findings
}

const canopyEvidenceBaseline = "d3c2692d0366e3dbdecf01a92d84aeeed4ba1ba8"

// ReleaseContext is the explicitly supplied deployment state for CNPY004.
// It is intentionally not inferred from repository contents or network data.
type ReleaseContext struct {
	Supplied                bool
	DeploymentHeight        uint64
	ActivationHeight        uint64
	RequiredProtocolVersion uint64
}

// EvaluateForkDrift classifies immutable upstream drift evidence. It can block
// only for a candidate version below an active required protocol version.
func EvaluateForkDrift(input upstream.Snapshot, context ReleaseContext) []diagnostic.Finding {
	findings := make([]diagnostic.Finding, 0, len(input.ChangedSensitivePaths)+2)
	if !context.Supplied {
		findings = append(findings, diagnostic.Finding{
			RuleID: "CNPY004", Code: "CNPY004-CONTEXT-REVIEW", Severity: diagnostic.SeverityMedium,
			Decision: diagnostic.DecisionReview, Summary: "Deployment or activation context is missing.",
			Details:           driftEvidence(input) + " Deployment, activation, and required protocol-version context was not supplied.",
			EvidenceLocations: []diagnostic.EvidenceLocation{{Path: "fsm/state.go", Line: input.ProtocolVersionLine, Detail: "committed candidate protocol-version declaration"}},
			Remediation:       "Supply heights and the required protocol version before release.", Confidence: diagnostic.ConfidenceHigh,
		})
	} else if context.DeploymentHeight >= context.ActivationHeight && input.CandidateProtocolVersion < context.RequiredProtocolVersion {
		findings = append(findings, diagnostic.Finding{
			RuleID: "CNPY004", Code: "CNPY004-ACTIVE-VERSION", Severity: diagnostic.SeverityHigh,
			Decision:          diagnostic.DecisionBlock,
			Summary:           fmt.Sprintf("Candidate protocol version %d is below active required version %d at height %d. Upgrade the fork before deployment.", input.CandidateProtocolVersion, context.RequiredProtocolVersion, context.DeploymentHeight),
			Details:           driftEvidence(input) + contextEvidence(context),
			EvidenceLocations: []diagnostic.EvidenceLocation{{Path: "fsm/state.go", Line: input.ProtocolVersionLine, Detail: "committed candidate CurrentProtocolVersion uint literal"}},
			Remediation:       "Upgrade the fork before deployment.", Confidence: diagnostic.ConfidenceHigh,
		})
	}
	for _, path := range input.ChangedSensitivePaths {
		findings = append(findings, diagnostic.Finding{
			RuleID: "CNPY004", Code: "CNPY004-SENSITIVE-DRIFT", Severity: diagnostic.SeverityMedium,
			Decision:          diagnostic.DecisionReview,
			Summary:           fmt.Sprintf("Compatibility-sensitive path %s differs between %s and %s. Review the change for an equivalent fork implementation.", path, input.BaseSHA, input.TargetSHA),
			Details:           driftEvidence(input) + contextEvidence(context),
			EvidenceLocations: []diagnostic.EvidenceLocation{{Path: path, Detail: "exact evidence-reviewed upstream path difference"}},
			Remediation:       "Review the change for an equivalent fork implementation.", Confidence: diagnostic.ConfidenceHigh,
		})
	}
	if len(findings) == 0 {
		findings = append(findings, diagnostic.Finding{
			RuleID: "CNPY004", Code: "CNPY004-CLEAN", Severity: diagnostic.SeverityInfo,
			Decision: diagnostic.DecisionPass, Summary: "No supported compatibility-sensitive drift detected.",
			Details:           driftEvidence(input) + contextEvidence(context),
			EvidenceLocations: []diagnostic.EvidenceLocation{{Path: "fsm/state.go", Line: input.ProtocolVersionLine, Detail: "committed candidate CurrentProtocolVersion uint literal"}},
			Remediation:       "Retain immutable comparison inputs for release review.", Confidence: diagnostic.ConfidenceHigh,
		})
	}
	diagnostic.SortFindings(findings)
	return findings
}

func driftEvidence(input upstream.Snapshot) string {
	return fmt.Sprintf("Base SHA: %s. Target SHA: %s. Candidate SHA: %s. Candidate protocol version: %d.", input.BaseSHA, input.TargetSHA, input.CandidateSHA, input.CandidateProtocolVersion)
}

func contextEvidence(context ReleaseContext) string {
	if !context.Supplied {
		return ""
	}
	return fmt.Sprintf(" Deployment height: %d. Activation height: %d. Required protocol version: %d.", context.DeploymentHeight, context.ActivationHeight, context.RequiredProtocolVersion)
}

func evaluateProtectedAccount(input evidence.Result) []diagnostic.Finding {
	var findings []diagnostic.Finding
	schemas := input.MessageSchemas["types.Account"]
	if input.Collected {
		if !input.SchemaSelection.Attempted {
			return nil
		}
		if !input.SchemaSelection.Proven {
			return []diagnostic.Finding{review("CNPY003", "CNPY003-ACCOUNT-UNKNOWN", input.SchemaSelection.Loc,
				"The candidate descriptor set cannot be established.",
				fmt.Sprintf("Descriptor initialization is unsupported or ambiguous; compatibility with Canopy baseline %s cannot be determined.", canopyEvidenceBaseline),
				"Use the documented deterministic descriptor initialization pattern.")}
		}
		for _, issue := range input.SchemaSelection.Issues {
			if issue.Malformed {
				findings = append(findings, block("CNPY003", "CNPY003-INVALID-DESCRIPTOR", issue.Loc,
					"The selected protobuf descriptor set is invalid.",
					fmt.Sprintf("%s. Canopy baseline: %s.", issue.Detail, canopyEvidenceBaseline),
					"Regenerate the selected descriptors and include their complete dependency closure."))
			} else {
				findings = append(findings, review("CNPY003", "CNPY003-ACCOUNT-UNKNOWN", issue.Loc,
					"The selected protobuf descriptor set needs review.", issue.Detail+".",
					"Use only directly resolvable generated descriptors in the initialization pipeline."))
			}
		}
		for _, finding := range findings {
			if finding.Decision == diagnostic.DecisionBlock {
				return findings
			}
		}
		schemas = input.SelectedSchemas["types.Account"]
	}
	if len(schemas) == 0 {
		if input.Collected && input.SchemaSelection.Proven && len(findings) == 0 {
			findings = append(findings, review("CNPY003", "CNPY003-ACCOUNT-UNKNOWN", input.SchemaSelection.Loc,
				"The selected descriptor set does not establish types.Account compatibility.",
				fmt.Sprintf("No selected descriptor declares types.Account; preservation behavior cannot be established against Canopy baseline %s.", canopyEvidenceBaseline),
				"Include the canonical types.Account descriptor or document and verify the complete unknown-field preservation path."))
		}
		return findings
	}
	if len(schemas) > 1 {
		locations := make([]diagnostic.EvidenceLocation, 0, len(schemas))
		for _, schema := range schemas {
			locations = append(locations, loc(schema.Loc, "duplicate types.Account descriptor"))
		}
		findings = append(findings, diagnostic.Finding{
			RuleID: "CNPY003", Code: "CNPY003-ACCOUNT-UNKNOWN", Severity: diagnostic.SeverityMedium,
			Decision: diagnostic.DecisionReview, Summary: "The candidate Account descriptor is ambiguous.",
			Details:           fmt.Sprintf("Found %d declarations of types.Account; compatibility with field 7 cannot be established against Canopy baseline %s.", len(schemas), canopyEvidenceBaseline),
			EvidenceLocations: locations, Remediation: "Provide one canonical types.Account descriptor and remove conflicting declarations.", Confidence: diagnostic.ConfidenceHigh,
		})
		return findings
	}
	schema := schemas[0]
	var protected []evidence.ProtoField
	for _, field := range schema.Fields {
		if field.Number == 7 {
			protected = append(protected, field)
		}
	}
	if len(protected) == 0 {
		findings = append(findings, review("CNPY003", "CNPY003-NONCE-REVIEW", schema.Loc,
			"types.Account does not declare the protected nonce field.",
			fmt.Sprintf("Field 7 is absent; field 10 is Transaction.nonce and is not a substitute. Descriptor omission does not prove loss because unknown fields may be preserved. Baseline: %s.", canopyEvidenceBaseline),
			"Confirm that the complete decode-modify-encode path preserves unknown field 7, or declare nonce as singular uint64 field 7."))
		return findings
	}
	if len(protected) > 1 {
		findings = append(findings, block("CNPY003", "CNPY003-NONCE-CONFLICT", schema.Loc,
			"types.Account reuses protected field number 7.",
			fmt.Sprintf("Field 7 appears %d times; Canopy baseline %s requires exactly one singular uint64 nonce.", len(protected), canopyEvidenceBaseline),
			"Regenerate types.Account with exactly one singular uint64 nonce at field 7."))
		return findings
	}
	field := protected[0]
	if field.Name != "nonce" || field.Kind != 4 || field.Label != 1 {
		findings = append(findings, block("CNPY003", "CNPY003-NONCE-CONFLICT", schema.Loc,
			"types.Account field 7 is incompatible with Canopy's nonce.",
			fmt.Sprintf("Observed field 7 name=%s, type=%s, cardinality=%s; baseline %s requires name nonce, singular uint64.", safeText(field.Name), protoKind(field.Kind), protoLabel(field.Label), canopyEvidenceBaseline),
			"Declare types.Account.nonce as singular uint64 field 7 and regenerate the protobuf code."))
		return findings
	}
	return findings
}

func protoKind(value uint64) string {
	names := map[uint64]string{1: "double", 2: "float", 3: "int64", 4: "uint64", 5: "int32", 6: "fixed64", 7: "fixed32", 8: "bool", 9: "string", 10: "group", 11: "message", 12: "bytes", 13: "uint32", 14: "enum", 15: "sfixed32", 16: "sfixed64", 17: "sint32", 18: "sint64"}
	if name, ok := names[value]; ok {
		return name
	}
	return fmt.Sprintf("unknown(%d)", value)
}

func protoLabel(value uint64) string {
	switch value {
	case 1:
		return "singular"
	case 2:
		return "required"
	case 3:
		return "repeated"
	default:
		return fmt.Sprintf("unknown(%d)", value)
	}
}

func evaluateRegistry(config evidence.Config, input evidence.Result) []diagnostic.Finding {
	var findings []diagnostic.Finding
	if config.Dynamic {
		return []diagnostic.Finding{review("CNPY001", "CNPY001-CONFIG-REVIEW", config.Loc,
			"Plugin configuration uses an unsupported composite form.", "The ContractConfig fields cannot be associated safely without type-checking target code.",
			"Use one keyed PluginConfig composite literal with supported literal registry fields.")}
	}
	if config.Mutated {
		findings = append(findings, review("CNPY001", "CNPY001-CONFIG-REVIEW", config.Loc,
			"Plugin configuration is mutated after declaration.", "Static literal values may not be the values sent during the plugin handshake.",
			"Keep the checked registry values immutable or generate a literal ContractConfig."))
		return findings
	}
	left, right := config.SupportedTransactions, config.TransactionTypeURLs
	if left.Dynamic || right.Dynamic {
		where := left.Loc
		if right.Dynamic {
			where = right.Loc
		}
		findings = append(findings, review("CNPY001", "CNPY001-CONFIG-REVIEW", where,
			"Transaction registry uses an unsupported dynamic expression.", "The ordered registry values cannot be recovered without executing target code.",
			"Use direct []string literals or nil for SupportedTransactions and TransactionTypeUrls."))
		return findings
	}
	if len(left.Values) != len(right.Values) {
		findings = append(findings, block("CNPY001", "CNPY001-COUNT", config.Loc,
			"Transaction registry list lengths do not match.",
			fmt.Sprintf("Transaction registry has %d names but %d type URLs.", len(left.Values), len(right.Values)),
			"Make both ordered lists the same length."))
		return findings
	}
	if len(left.Values) == 0 {
		return findings
	}
	selectedDescriptors := input.Collected && input.SchemaSelection.Attempted && input.SchemaSelection.Proven && len(input.SchemaSelection.Issues) == 0
	for index, value := range right.Values {
		safe := safeText(value)
		name, valid := typeURLName(value)
		if !valid {
			findings = append(findings, block("CNPY001", "CNPY001-TYPE-URL", right.Loc,
				fmt.Sprintf("Transaction type URL at index %d is malformed.", index),
				fmt.Sprintf("Index %d does not contain a non-empty protobuf message name: %s.", index, safe),
				"Use a type URL whose final path segment is a fully qualified protobuf message name."))
			continue
		}
		if selectedDescriptors {
			schemas := input.SelectedSchemas[name]
			switch len(schemas) {
			case 0:
				findings = append(findings, block("CNPY001", "CNPY001-TYPE-URL", right.Loc,
					fmt.Sprintf("Transaction type URL at index %d does not resolve to a protobuf message.", index),
					fmt.Sprintf("No message in the proven selected descriptor set matches %s at index %d.", safe, index),
					"Include the transaction message descriptor in ContractConfig.FileDescriptorProtos or correct the type URL."))
			case 1:
				// The D015-proven selected descriptor set is the authoritative
				// view of the descriptors advertised by the plugin.
			default:
				findings = append(findings, review("CNPY001", "CNPY001-DESCRIPTOR-REVIEW", right.Loc,
					fmt.Sprintf("Transaction type URL at index %d resolves ambiguously.", index),
					fmt.Sprintf("The proven selected descriptor set contains %d messages named %s.", len(schemas), safe),
					"Regenerate the selected descriptors so every protobuf message name is unique."))
			}
			continue
		}
		if _, found := input.Messages[name]; input.DescriptorCount > 0 && !found {
			findings = append(findings, review("CNPY001", "CNPY001-DESCRIPTOR-REVIEW", right.Loc,
				fmt.Sprintf("Transaction type URL at index %d was not found in inspected descriptors.", index),
				fmt.Sprintf("No inspected top-level message matches %s, but descriptor handshake provenance is not yet proven.", safe),
				"Confirm the generated descriptor is included in ContractConfig.FileDescriptorProtos."))
		}
	}
	if !selectedDescriptors {
		for _, issue := range input.SchemaSelection.Issues {
			findings = append(findings, review("CNPY001", "CNPY001-DESCRIPTOR-REVIEW", issue.Loc,
				"Selected descriptor data cannot be used as registry authority.", issue.Detail+"; transaction type URL resolution remains unproven.",
				"Use the documented deterministic descriptor initialization pattern and include a complete valid descriptor set."))
		}
		for _, issue := range input.DescriptorIssues {
			findings = append(findings, review("CNPY001", "CNPY001-DESCRIPTOR-REVIEW", issue.Loc,
				"Plugin descriptor data cannot be inspected completely.", issue.Detail+"; its inclusion in the handshake is not proven.",
				"Regenerate descriptors using supported string-literal raw descriptor output and confirm the handshake assignment."))
		}
		if input.DescriptorCount == 0 {
			findings = append(findings, review("CNPY001", "CNPY001-DESCRIPTOR-REVIEW", config.Loc,
				"Transaction descriptors are unavailable.", "Registry counts match, but type URL resolution cannot be validated without supported descriptor data.",
				"Include generated .pb.go raw descriptors so every type URL can be resolved."))
		} else {
			findings = append(findings, review("CNPY001", "CNPY001-DESCRIPTOR-REVIEW", config.Loc,
				"Descriptor handshake provenance needs review.", "Generated descriptors were inspected, but the selected descriptor set is unattempted, unproven, ambiguous, or issue-bearing.",
				"Use the documented deterministic descriptor initialization pattern and include every transaction descriptor in the plugin handshake."))
		}
	}
	findings = append(findings, duplicateReviews(left.Values, "transaction name", left.Loc)...)
	findings = append(findings, duplicateReviews(right.Values, "transaction type URL", right.Loc)...)
	return findings
}

func evaluatePrefixes(config evidence.Config) []diagnostic.Finding {
	var findings []diagnostic.Finding
	if config.Dynamic {
		return []diagnostic.Finding{review("CNPY002", "CNPY002-PREFIX-REVIEW", config.Loc,
			"Plugin configuration uses an unsupported composite form.", "CustomStatePrefixes cannot be associated safely with a keyed field.",
			"Use one keyed PluginConfig composite literal with a supported literal prefix list.")}
	}
	values := config.CustomStatePrefixes
	if config.Mutated {
		return []diagnostic.Finding{review("CNPY002", "CNPY002-PREFIX-REVIEW", config.Loc,
			"Custom state prefixes are mutated after declaration.", "The declared literal may not match the configuration sent during the plugin handshake.",
			"Keep CustomStatePrefixes immutable and directly inspectable.")}
	}
	if values.Dynamic {
		return []diagnostic.Finding{review("CNPY002", "CNPY002-PREFIX-REVIEW", values.Loc,
			"Custom state prefixes use an unsupported dynamic expression.", "Exact prefix bytes cannot be recovered without executing target code.",
			"Use a direct [][]byte or [][]uint8 literal.")}
	}
	for _, prefix := range values.Values {
		if prefix.Dynamic || len(prefix.Bytes) == 0 {
			findings = append(findings, review("CNPY002", "CNPY002-PREFIX-REVIEW", prefix.Loc,
				"A custom state prefix cannot be classified safely.", "The declaration is empty, dynamic, ambiguous, or outside the supported literal representation.",
				"Declare the exact non-empty prefix bytes using a supported literal."))
			continue
		}
		if len(prefix.Bytes) == 1 && prefix.Bytes[0] >= 1 && prefix.Bytes[0] <= 15 {
			hexValue := evidence.PrefixHex(prefix.Bytes)
			findings = append(findings, block("CNPY002", "CNPY002-DECLARED-COLLISION", prefix.Loc,
				"Custom state prefix collides with Canopy core state.",
				fmt.Sprintf("Custom prefix %s has length 1 and is reserved by Canopy core.", hexValue),
				"Choose a plugin-owned prefix outside the one-byte range 1 through 15."))
		}
	}
	return findings
}

func duplicateReviews(values []string, label string, where evidence.Location) []diagnostic.Finding {
	seen := make(map[string]int)
	var findings []diagnostic.Finding
	for index, value := range values {
		if first, exists := seen[value]; exists {
			findings = append(findings, review("CNPY001", "CNPY001-DUPLICATE-REVIEW", where,
				fmt.Sprintf("Duplicate %s needs review.", label),
				fmt.Sprintf("Indices %d and %d contain the same %s %s.", first, index, label, safeText(value)),
				"Confirm every positional registry entry is intentional and unique."))
		} else {
			seen[value] = index
		}
	}
	return findings
}

func typeURLName(value string) (string, bool) {
	if value == "" || strings.TrimSpace(value) != value || strings.IndexFunc(value, unicode.IsControl) >= 0 {
		return "", false
	}
	name := value
	if index := strings.LastIndexByte(value, '/'); index >= 0 {
		name = value[index+1:]
	}
	parts := strings.Split(name, ".")
	if len(parts) < 1 {
		return "", false
	}
	for _, part := range parts {
		if part == "" || !identifier(part) {
			return "", false
		}
	}
	return name, true
}

func identifier(value string) bool {
	for index, r := range value {
		if !(r == '_' || unicode.IsLetter(r) || (index > 0 && unicode.IsDigit(r))) {
			return false
		}
	}
	return true
}

func block(ruleID, code string, where evidence.Location, summary, details, remediation string) diagnostic.Finding {
	return diagnostic.Finding{RuleID: ruleID, Code: code, Severity: diagnostic.SeverityHigh, Decision: diagnostic.DecisionBlock,
		Summary: summary, Details: details, EvidenceLocations: []diagnostic.EvidenceLocation{loc(where, "deterministic source evidence")},
		Remediation: remediation, Confidence: diagnostic.ConfidenceHigh}
}

func review(ruleID, code string, where evidence.Location, summary, details, remediation string) diagnostic.Finding {
	return diagnostic.Finding{RuleID: ruleID, Code: code, Severity: diagnostic.SeverityMedium, Decision: diagnostic.DecisionReview,
		Summary: summary, Details: details, EvidenceLocations: []diagnostic.EvidenceLocation{loc(where, "incomplete static evidence")},
		Remediation: remediation, Confidence: diagnostic.ConfidenceHigh}
}

func loc(where evidence.Location, detail string) diagnostic.EvidenceLocation {
	path := where.Path
	if path == "" {
		path = "."
	}
	return diagnostic.EvidenceLocation{Path: path, Line: where.Line, Detail: detail}
}

func safeText(value string) string {
	value = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, value)
	const limit = 160
	runes := []rune(value)
	if len(runes) > limit {
		value = string(runes[:limit]) + "..."
	}
	return fmt.Sprintf("%q", value)
}

// StableMessageNames is exposed only as a deterministic diagnostic aid.
func StableMessageNames(input evidence.Result) []string {
	names := make([]string, 0, len(input.Messages))
	for name := range input.Messages {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
