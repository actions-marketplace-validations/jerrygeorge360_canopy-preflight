# Evidence-Locked Rule Contracts

These contracts define what the first Canopy Doctor release may claim. All
Canopy source references are pinned to commit
`d3c2692d0366e3dbdecf01a92d84aeeed4ba1ba8`.

The implementation must preserve three decisions:

- `PASS` means no problem was found within the rule's documented scope.
- `REVIEW REQUIRED` means the available static evidence is incomplete or
  ambiguous.
- `DO NOT RELEASE` is reserved for a deterministic violation supported by the
  evidence in the applicable rule contract.

Rules use one evaluation precedence so outcomes cannot overlap:

1. malformed or unverifiable required input produces an actionable error;
2. a proven blocking condition produces `DO NOT RELEASE`;
3. remaining uncertainty produces `REVIEW REQUIRED`;
4. `PASS` is available only when none of the earlier conditions applies.

The inspected repository is untrusted data. Collectors may parse files and Git
objects, but must not execute target code, generators, hooks, tests, or binaries.

| Contract | Initial scope |
| --- | --- |
| [CNPY001](CNPY001.md) | Transaction registry structure and descriptor resolution |
| [CNPY002](CNPY002.md) | Declared reserved-prefix collisions |
| [CNPY003](CNPY003.md) | Descriptor validity and protected `Account.nonce` representation |
| [CNPY004](CNPY004.md) | Pinned-ref sensitive drift and active protocol-version compatibility |
