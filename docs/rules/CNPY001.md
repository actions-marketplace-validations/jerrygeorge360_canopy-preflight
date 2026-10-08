# CNPY001 - Transaction Registry Consistency

## Purpose

Validate the positional transaction registry supplied by a Canopy plugin.
Canopy treats `supported_transactions` and `transaction_type_urls` as parallel
arrays and resolves each advertised type URL through the supplied protobuf file
descriptors.

## Immutable evidence

Evidence baseline: Canopy commit
[`d3c2692d0366e3dbdecf01a92d84aeeed4ba1ba8`](https://github.com/canopy-network/canopy/commit/d3c2692d0366e3dbdecf01a92d84aeeed4ba1ba8).

- [`lib/.proto/plugin.proto`, lines 79-98](https://github.com/canopy-network/canopy/blob/d3c2692d0366e3dbdecf01a92d84aeeed4ba1ba8/lib/.proto/plugin.proto#L79-L98) documents that the two arrays must have matching order.
- [`lib/codec.go`, lines 117-176](https://github.com/canopy-network/canopy/blob/d3c2692d0366e3dbdecf01a92d84aeeed4ba1ba8/lib/codec.go#L117-L176) checks counts and resolves message descriptors.
- [`lib/plugin.go`, lines 227-241](https://github.com/canopy-network/canopy/blob/d3c2692d0366e3dbdecf01a92d84aeeed4ba1ba8/lib/plugin.go#L227-L241) consumes the configured registry.
- [`plugin/go/TUTORIAL.md`, lines 64-85](https://github.com/canopy-network/canopy/blob/d3c2692d0366e3dbdecf01a92d84aeeed4ba1ba8/plugin/go/TUTORIAL.md#L64-L85) gives the builder-facing configuration requirement.
- The behavior was introduced or clarified by commit
  [`0a629f51c1a6ec7f31379fa5fa3cdbb5e6813623`](https://github.com/canopy-network/canopy/commit/0a629f51c1a6ec7f31379fa5fa3cdbb5e6813623).
- The official Go template at commit
  [`5820274b55dcc22b08c48ed7e4c76d953c5e0da6`](https://github.com/canopy-network/canopy/commit/5820274b55dcc22b08c48ed7e4c76d953c5e0da6)
  declares `ContractConfig` in `plugin/go/contract/contract.go`, stores that
  pointer in `Plugin.pluginConfig`, and sends it through
  `PluginToFSM_Config.Config` in `(*Plugin).Handshake`.

## Inputs and normalization

- plugin configuration containing both arrays;
- serialized `FileDescriptorProto` entries when provided;
- normalized type URLs and deterministic source locations;
- array order must be retained exactly as declared.

## Decision table

Evaluate rows using the precedence in `docs/rules/README.md`. `PASS` requires
valid, complete inputs and no blocking or review condition.

| Observed condition | Decision | Confidence |
| --- | --- | --- |
| Both arrays are empty | `PASS` | High |
| Counts match, every non-empty type URL resolves to a message descriptor, and no review condition applies | `PASS` | High |
| The configuration uses the exact bounded official Go pointer-to-handshake flow and the global use census finds no extra alias or mutation | Continue evaluating the declared arrays | High |
| Supported transactions are non-empty but counts differ | `DO NOT RELEASE` | High |
| A type URL is empty, malformed, unresolved, or resolves to a non-message descriptor | `DO NOT RELEASE` | High |
| Descriptor data is malformed | `DO NOT RELEASE` | High |
| Descriptors are absent, so semantic registration cannot be validated | `REVIEW REQUIRED` | High |
| Entries are duplicated | `REVIEW REQUIRED` | Medium |
| Names appear reversed but no authoritative name-to-URL mapping is available | `REVIEW REQUIRED` | Medium |

## Finding requirements

The finding must include the array indices, the two declared values, the source
location, the failed structural check, and a remediation. It must not infer a
semantic mismatch from naming similarity alone.

Plain-English message templates:

- `CNPY001-COUNT`: "Transaction registry has {name_count} names but
  {url_count} type URLs. Make both ordered lists the same length."
- `CNPY001-DESCRIPTOR`: "Plugin descriptor data cannot be parsed and linked:
  {reason}. Regenerate or repair the descriptor set."
- `CNPY001-TYPE-URL`: "Transaction type URL at index {index} does not resolve
  to a protobuf message: {type_url}."
- `CNPY001-ORDER-REVIEW`: "Registry entry {index} may not match its intended
  message. Confirm the positional mapping."

## Fixtures

- valid: equal arrays whose URLs resolve to message descriptors;
- broken: unequal array lengths;
- broken: unresolved type URL;
- review: arrays that look reversed but remain structurally resolvable;
- malformed: invalid descriptor bytes.

## Limitations and false positives

Canopy's published interface establishes positional alignment, but static names
alone do not prove which transaction identifier belongs to which descriptor.
The first release therefore cannot hard-fail a suspected order reversal without
an authoritative mapping.

The official pointer flow is accepted only as the complete D020 witness. A
similar-looking field assignment, wrapper detour, extra pointer use, method
call, or unproven send path remains `REVIEW REQUIRED`.

The D020 proof ends when the exact deterministic marshalled bytes are passed to
`sendLengthPrefixed`. Transport delivery and listener liveness are outside this
static rule's scope.

## Acceptance criteria

- All decision-table rows have deterministic tests.
- Human and JSON output identify the same indices and outcome.
- Input parsing never executes plugin code.
- Finding order is stable across repeated runs.
