# CNPY004 - Protocol-Aware Fork Drift

## Purpose

Compare a candidate fork using explicit immutable Canopy commits, detect a
direct active protocol-version incompatibility, and route changes in exact
reviewed compatibility-sensitive files to human review.

## Immutable evidence

Evidence baseline: Canopy commit
[`d3c2692d0366e3dbdecf01a92d84aeeed4ba1ba8`](https://github.com/canopy-network/canopy/commit/d3c2692d0366e3dbdecf01a92d84aeeed4ba1ba8).

- [`fsm/state.go`, lines 16-21 and 74-87](https://github.com/canopy-network/canopy/blob/d3c2692d0366e3dbdecf01a92d84aeeed4ba1ba8/fsm/state.go#L16-L87) defines and initializes the compiled protocol version.
- [`fsm/automatic.go`, lines 118-134](https://github.com/canopy-network/canopy/blob/d3c2692d0366e3dbdecf01a92d84aeeed4ba1ba8/fsm/automatic.go#L118-L134) rejects older software after activation.
- [`fsm/automatic.md`](https://github.com/canopy-network/canopy/blob/d3c2692d0366e3dbdecf01a92d84aeeed4ba1ba8/fsm/automatic.md) documents automatic protocol behavior.
- [`fsm/gov.go`, lines 511-540](https://github.com/canopy-network/canopy/blob/d3c2692d0366e3dbdecf01a92d84aeeed4ba1ba8/fsm/gov.go#L511-L540) and [`fsm/gov.md`, lines 71-105](https://github.com/canopy-network/canopy/blob/d3c2692d0366e3dbdecf01a92d84aeeed4ba1ba8/fsm/gov.md#L71-L105) establish governance-controlled activation context.

Exact review paths:

| Path | Evidence-backed reason |
| --- | --- |
| `fsm/state.go` | Defines and initializes `CurrentProtocolVersion`; cited above |
| `fsm/automatic.go` | Enforces the required version after activation; cited above |
| `fsm/gov.go` | Applies governance version and activation behavior; cited above |
| `fsm/gov_params.go` | [Defines governance parameters used by the FSM](https://github.com/canopy-network/canopy/blob/d3c2692d0366e3dbdecf01a92d84aeeed4ba1ba8/fsm/gov_params.go) |
| `fsm/transaction.go` | [Implements transaction validation and execution](https://github.com/canopy-network/canopy/blob/d3c2692d0366e3dbdecf01a92d84aeeed4ba1ba8/fsm/transaction.go) |
| `fsm/ethereum.go` | [Implements Ethereum transaction translation](https://github.com/canopy-network/canopy/blob/d3c2692d0366e3dbdecf01a92d84aeeed4ba1ba8/fsm/ethereum.go) |
| `fsm/key.go` | [Defines core state-key prefixes](https://github.com/canopy-network/canopy/blob/d3c2692d0366e3dbdecf01a92d84aeeed4ba1ba8/fsm/key.go#L30-L45) |
| `lib/.proto/account.proto` | [Defines canonical shared Account state](https://github.com/canopy-network/canopy/blob/d3c2692d0366e3dbdecf01a92d84aeeed4ba1ba8/lib/.proto/account.proto#L26-L40) |
| `lib/.proto/tx.proto` | [Defines the canonical transaction envelope](https://github.com/canopy-network/canopy/blob/d3c2692d0366e3dbdecf01a92d84aeeed4ba1ba8/lib/.proto/tx.proto#L28-L44) |
| `lib/.proto/plugin.proto` | [Defines the plugin handshake and state I/O contract](https://github.com/canopy-network/canopy/blob/d3c2692d0366e3dbdecf01a92d84aeeed4ba1ba8/lib/.proto/plugin.proto#L79-L98) |
| `lib/plugin.go` | [Implements handshake, registry, and prefix guards](https://github.com/canopy-network/canopy/blob/d3c2692d0366e3dbdecf01a92d84aeeed4ba1ba8/lib/plugin.go#L292-L365) |
| `lib/codec.go` | [Implements descriptor registration](https://github.com/canopy-network/canopy/blob/d3c2692d0366e3dbdecf01a92d84aeeed4ba1ba8/lib/codec.go#L117-L205) |

No glob or inferred mirror path is classified. Expanding this exact
list requires another evidence review.

## Inputs and normalization

- full immutable base, target, and candidate commit SHAs;
- complete Git history needed to compare those commits;
- explicitly supplied deployment height, activation height, and required
  protocol version when a release decision is requested;
- candidate compiled protocol version from the supported static source form;
- deterministic Git diff metadata collected without hooks or code execution.

Only committed candidates are supported. Dirty worktrees and moving
refs are actionable input errors until they are committed and resolved to SHAs.

## Decision table

Evaluate rows using the precedence in `docs/rules/README.md`. Missing deployment
context is reviewable; unresolved Git identity or history is an input error.

| Observed condition | Decision | Confidence |
| --- | --- | --- |
| All SHAs and history resolve, deployment context is supplied, candidate meets the active version, no exact review path differs, and no earlier condition applies | `PASS` | High |
| An exact reviewed path differs | `REVIEW REQUIRED` | High |
| Deployment height, activation height, or required protocol version is missing | `REVIEW REQUIRED` | High |
| Refs are moving, ambiguous, unresolved, or history is shallow/incomplete | actionable input error; no release decision | High |
| Candidate compiled version is below the required version at or after activation | `DO NOT RELEASE` | High |

## Finding requirements

The report must include all immutable SHAs, path or protocol-version evidence,
and supplied deployment context. A clean result must say: "No supported
compatibility-sensitive drift detected." It must not claim that the entire fork
is compatible.

Plain-English message templates:

- `CNPY004-ACTIVE-VERSION`: "Candidate protocol version {candidate} is below
  active required version {required} at height {height}. Upgrade the fork before
  deployment."
- `CNPY004-SENSITIVE-DRIFT`: "Compatibility-sensitive path {path} differs
  between {base_sha} and {target_sha}. Review the change for an equivalent fork
  implementation."
- `CNPY004-CONTEXT-REVIEW`: "Deployment or activation context is missing.
  Supply heights and the required protocol version before release."
- `CNPY004-INPUT`: "The comparison cannot be reproduced: {reason}. Supply full
  reachable commit SHAs and complete history."

## Fixtures

- valid: pinned commits, complete context, no exact sensitive drift, compatible version;
- review: a change to every exact sensitive-path class;
- review: missing activation context;
- error: symbolic or unresolved ref, dirty candidate, or shallow history;
- broken: candidate version 1 when version 2 is active at the comparison height;
- boundary: immediately before, at, and after activation height;
- ordinary: documentation-only drift does not become a protocol failure.

## Limitations and false positives

Git distance and path contact do not prove incompatibility. A fork may implement
an equivalent change using different commits. The rule is conservative: path
drift requests review, and only a direct active protocol-version mismatch can
block release. The tool does not fetch live governance state in its offline core.

## Acceptance criteria

- Only full, reachable commit SHAs are accepted as repository identities.
- Ordinary commit distance is never presented as a protocol failure.
- Exact-path results are deterministically sorted; no undocumented glob matches.
- Missing network or Git data yields an actionable, non-misleading result.
- Activation behavior is tested immediately before, at, and after the boundary.
- Human and JSON output contain the same finding codes, evidence, and decision.
