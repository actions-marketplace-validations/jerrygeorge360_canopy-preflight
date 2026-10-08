# CNPY003 - Protected Protobuf State Compatibility

## Purpose

Validate plugin protobuf descriptors and detect an incompatible representation
of a protected core-shared field. The initial protected scenario is
`types.Account.nonce`, field **7**, of type `uint64`.

## Immutable evidence

Evidence baseline: Canopy commit
[`d3c2692d0366e3dbdecf01a92d84aeeed4ba1ba8`](https://github.com/canopy-network/canopy/commit/d3c2692d0366e3dbdecf01a92d84aeeed4ba1ba8).

- [`lib/.proto/account.proto`, lines 26-40](https://github.com/canopy-network/canopy/blob/d3c2692d0366e3dbdecf01a92d84aeeed4ba1ba8/lib/.proto/account.proto#L26-L40) defines `types.Account.nonce` as field 7, `uint64`.
- [`lib/.proto/tx.proto`, lines 28-44](https://github.com/canopy-network/canopy/blob/d3c2692d0366e3dbdecf01a92d84aeeed4ba1ba8/lib/.proto/tx.proto#L28-L44) distinguishes `types.Transaction.nonce` as field 10.
- [`fsm/ethereum.md`, lines 289-301](https://github.com/canopy-network/canopy/blob/d3c2692d0366e3dbdecf01a92d84aeeed4ba1ba8/fsm/ethereum.md#L289-L301) describes nonce behavior.
- [`fsm/ethereum.md`, lines 320-326](https://github.com/canopy-network/canopy/blob/d3c2692d0366e3dbdecf01a92d84aeeed4ba1ba8/fsm/ethereum.md#L320-L326) warns that plugins must understand the nonce or preserve unknown protobuf fields to avoid mixed-version divergence.
- [`lib/.proto/plugin.proto`, lines 79-98](https://github.com/canopy-network/canopy/blob/d3c2692d0366e3dbdecf01a92d84aeeed4ba1ba8/lib/.proto/plugin.proto#L79-L98), [`plugin/go/contract/contract.go`, lines 18-48](https://github.com/canopy-network/canopy/blob/d3c2692d0366e3dbdecf01a92d84aeeed4ba1ba8/plugin/go/contract/contract.go#L18-L48), and [`plugin/go/contract/plugin.go`, lines 79-96](https://github.com/canopy-network/canopy/blob/d3c2692d0366e3dbdecf01a92d84aeeed4ba1ba8/plugin/go/contract/plugin.go#L79-L96) establish descriptor transport.
- [`lib/plugin.go`, lines 292-338](https://github.com/canopy-network/canopy/blob/d3c2692d0366e3dbdecf01a92d84aeeed4ba1ba8/lib/plugin.go#L292-L338) and [`lib/codec.go`, lines 117-205](https://github.com/canopy-network/canopy/blob/d3c2692d0366e3dbdecf01a92d84aeeed4ba1ba8/lib/codec.go#L117-L205) establish descriptor registration and validation.
- [`plugin/go/contract/account.pb.go`, lines 27-36](https://github.com/canopy-network/canopy/blob/d3c2692d0366e3dbdecf01a92d84aeeed4ba1ba8/plugin/go/contract/account.pb.go#L27-L36) shows why omission alone cannot establish loss: generated messages may retain unknown fields.
- Historical context: commits [`d19b7081bd8b62ac2402d0598e24dc84d7ff90dc`](https://github.com/canopy-network/canopy/commit/d19b7081bd8b62ac2402d0598e24dc84d7ff90dc), [`7cfb28fa4166df8af764b9784ad5a9c989b8f430`](https://github.com/canopy-network/canopy/commit/7cfb28fa4166df8af764b9784ad5a9c989b8f430), and [`3b338b24f35d48b0938570738e378ef8cbddd198`](https://github.com/canopy-network/canopy/commit/3b338b24f35d48b0938570738e378ef8cbddd198) record the RLP.V2 nonce changes.

## Inputs and normalization

- pinned upstream and candidate descriptor sets;
- an explicit protected-field policy, initially only fully qualified
  `types.Account.nonce = 7` with protobuf type `uint64` and singular cardinality;
- fully qualified message names and canonical descriptor ordering;
- bounded descriptor counts and total byte size.

Canopy Doctor neither executes candidate serializers nor trusts plugin-supplied
roundtrip claims.

## Decision table

Evaluate rows using the precedence in `docs/rules/README.md`. `PASS` requires
valid, complete descriptor evidence and no blocking or review condition.

| Observed condition | Decision | Confidence |
| --- | --- | --- |
| Descriptors parse and protected field 7 is explicitly compatible | `PASS` | High |
| Protected field 7 is reused, non-singular, or defined with a non-`uint64` type | `DO NOT RELEASE` | High |
| Descriptor data is malformed, cannot link, or an advertised message cannot resolve | `DO NOT RELEASE` | High |
| Candidate `types.Account` omits protected field 7 | `REVIEW REQUIRED` | High |
| The shared Account identity or preservation behavior cannot be established | `REVIEW REQUIRED` | High |

## Finding requirements

The finding must name the fully qualified message, field number, expected type,
observed descriptor evidence, and immutable comparison baseline.

Plain-English message templates:

- `CNPY003-INVALID-DESCRIPTOR`: "Plugin descriptors cannot be parsed and
  linked: {reason}. Repair the descriptor set before release."
- `CNPY003-NONCE-CONFLICT`: "types.Account field 7 is {observed_type}; Canopy
  requires a singular uint64 nonce at this field number."
- `CNPY003-NONCE-REVIEW`: "types.Account does not declare nonce field 7.
  Confirm that the complete codec path preserves unknown fields."
- `CNPY003-ACCOUNT-UNKNOWN`: "The candidate's shared Account identity or
  preservation behavior cannot be established from supported descriptors."

## Fixtures

- valid: descriptor explicitly contains `types.Account.nonce = 7` as singular `uint64`;
- broken: field 7 reused as an incompatible type or cardinality;
- review: older descriptor omits field 7;
- review: preservation is asserted but cannot be verified without executing the candidate;
- malformed: invalid or unresolvable descriptor graph;
- ambiguous: unrelated message with basename `Account` must not be treated as `types.Account`.

## Limitations and false positives

Descriptor omission does not itself prove state loss. Standard protobuf unknown
field handling may preserve fields absent from an older generated message.
Canopy Doctor neither executes the target codec nor trusts plugin-supplied roundtrip
claims. General field-removal and runtime-preservation analysis are outside the
first implementation.

## Acceptance criteria

- Every comparison is anchored to an immutable baseline and fully qualified name.
- Omission returns `REVIEW REQUIRED`, never automatic field-loss failure.
- Tests prevent regression to the incorrect claim that `Account.nonce` is field
  10; field 10 belongs to `Transaction.nonce`.
- Descriptor bounds, malformed input, dependency closure, and ambiguity are tested.
- Human and JSON output contain the same finding codes, evidence, and decision.
