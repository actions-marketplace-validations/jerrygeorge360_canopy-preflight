# CNPY002 - State Prefix Safety

## Purpose

Detect declared plugin prefixes that collide with Canopy's reserved one-byte
core prefixes. Runtime-write source analysis is deferred until an exact,
evidence-reviewed static-analysis contract exists.

## Immutable evidence

Evidence baseline: Canopy commit
[`d3c2692d0366e3dbdecf01a92d84aeeed4ba1ba8`](https://github.com/canopy-network/canopy/commit/d3c2692d0366e3dbdecf01a92d84aeeed4ba1ba8).

- [`lib/plugin.go`, lines 300-365](https://github.com/canopy-network/canopy/blob/d3c2692d0366e3dbdecf01a92d84aeeed4ba1ba8/lib/plugin.go#L300-L365) defines `CoreReservedPrefixMax` and rejects declared collisions during configuration.
- [`fsm/key.go`, lines 30-45](https://github.com/canopy-network/canopy/blob/d3c2692d0366e3dbdecf01a92d84aeeed4ba1ba8/fsm/key.go#L30-L45) defines the one-byte core prefix range 1 through 15.
- [`fsm/state.go`, lines 902-990](https://github.com/canopy-network/canopy/blob/d3c2692d0366e3dbdecf01a92d84aeeed4ba1ba8/fsm/state.go#L902-L990) guards plugin writes to core state while permitting shared account and pool access.
- [`fsm/plugin_guard_test.go`, lines 10-50](https://github.com/canopy-network/canopy/blob/d3c2692d0366e3dbdecf01a92d84aeeed4ba1ba8/fsm/plugin_guard_test.go#L10-L50) covers the runtime guard.
- [`lib/.proto/plugin.proto`, lines 95-98](https://github.com/canopy-network/canopy/blob/d3c2692d0366e3dbdecf01a92d84aeeed4ba1ba8/lib/.proto/plugin.proto#L95-L98) defines declared custom prefixes.
- The guard was introduced or clarified by commit
  [`7ca29b6188a986387acb3dc2e2dd4f11a50d4864`](https://github.com/canopy-network/canopy/commit/7ca29b6188a986387acb3dc2e2dd4f11a50d4864).
- The official Go template at commit
  [`5820274b55dcc22b08c48ed7e4c76d953c5e0da6`](https://github.com/canopy-network/canopy/commit/5820274b55dcc22b08c48ed7e4c76d953c5e0da6)
  sends the package-level `ContractConfig` pointer through the bounded
  `Plugin.pluginConfig` handshake path documented by D020.

## Inputs and normalization

- declared custom prefixes from the supported plugin configuration format;
- prefixes represented as byte strings, preserving their exact length;
- declaration bytes are not interpreted as encoded runtime keys;
- findings are sorted by source location and byte sequence.

## Decision table

Evaluate rows using the precedence in `docs/rules/README.md`. `PASS` requires
complete configuration input and no blocking or review condition.

| Observed condition | Decision | Confidence |
| --- | --- | --- |
| All declarations are readable and none equals a one-byte value from 1 through 15 | `PASS` | High |
| The configuration uses the exact bounded official Go pointer-to-handshake flow and the global use census finds no extra alias or mutation | Continue evaluating the declared prefixes | High |
| A declaration is exactly one byte in the range 1 through 15 | `DO NOT RELEASE` | High |
| A declaration is empty, dynamic, or cannot be decoded from the supported configuration representation | `REVIEW REQUIRED` | High |

## Finding requirements

The finding must display the declared prefix as a byte sequence and its length.
It must not label a multi-byte declaration as a collision based on its first
byte.

Plain-English message templates:

- `CNPY002-DECLARED-COLLISION`: "Custom prefix {prefix_hex} is a reserved
  one-byte Canopy prefix. Choose a plugin-owned prefix outside 1 through 15."
- `CNPY002-PREFIX-REVIEW`: "Custom prefix at {location} cannot be classified
  safely. Confirm its exact byte value before release."

## Fixtures

- valid boundaries: `[0]`, `[16]`, `[100]`, `[255]`, and `[1, 2]`;
- broken boundaries: declared single-byte `[1]`, `[7]`, and `[15]`;
- review: empty or dynamically constructed declaration.

## Limitations and false positives

The current Canopy runtime permits shared account and pool access and blocks
other reserved runtime writes, but static source occurrences do not prove which
keys reach those operations. Runtime-write and undeclared-write analysis is
outside the supported check. Multi-byte declarations must not be reduced to their first
byte.

Pointer-based configuration is accepted only when the complete D020 flow is
proven. Arbitrary pointer escape, aliasing, method calls, or carrier mutation
still requires review. The proof reaches the `sendLengthPrefixed` call boundary;
transport delivery and listener liveness are not claimed by this rule.

## Acceptance criteria

- Boundary fixtures prove byte-length-aware behavior.
- Single-byte declarations 1 through 15 block, including 1 and 2.
- Runtime key source analysis is not performed or represented as proven.
- Unsupported or dynamic declarations return `REVIEW REQUIRED`.
- Human and JSON results are identical and deterministic.
