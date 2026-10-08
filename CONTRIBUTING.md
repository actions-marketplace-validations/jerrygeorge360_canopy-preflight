# Contributing

Canopy Doctor accepts small, tested changes with clear Canopy evidence.

## Setup

Install the Go version declared in `go.mod`, Git, and Make. Then run:

```bash
make check
```

## Pull requests

Each pull request should:

1. explain the problem and the intended behavior;
2. include deterministic positive and negative tests;
3. identify affected rule or finding codes;
4. document security and compatibility effects;
5. pass `make check` and `git diff --check`.

Tests must work offline and must not depend on wall-clock time, map iteration
order, or machine-specific paths. Git tests should use temporary repositories
and full commit SHAs.

## Rule changes

A new or changed Canopy rule requires:

- a stable rule ID and finding code;
- a primary Canopy source pinned to an immutable commit;
- separate `PASS`, `REVIEW REQUIRED`, and `DO NOT RELEASE` conditions;
- documented uncertainty and false-positive limits;
- valid and intentionally broken fixtures;
- matching human and JSON assertions;
- a clear remediation message.

Incomplete evidence must not produce `DO NOT RELEASE`.

## Safety requirements

- Never execute code from the inspected project.
- Keep inspection read-only and bounded.
- Treat source files, descriptors, paths, and Git metadata as untrusted input.
- Keep network access explicit. The default checks must work offline.
- Do not include credentials, private chain state, or generated binaries.

See [`SECURITY.md`](SECURITY.md) for the complete inspection boundary.
