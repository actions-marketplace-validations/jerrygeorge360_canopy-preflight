# Canopy Doctor Demo

This walkthrough demonstrates the four rule families without claiming
that a passing result proves total upgrade safety.

## Prerequisites

- Go 1.26 or the version declared by `go.mod`;
- Git;
- a checkout of this repository.

Build the CLI once:

```bash
make build
```

The examples below assume the binary is `./bin/canopy-doctor`.

## 1. Check a supported plugin fixture

```bash
./bin/canopy-doctor check testdata/phase2/pass
```

Expected decision: `PASS`.

## 2. Detect a registry mismatch

```bash
./bin/canopy-doctor check testdata/phase2/block
```

Expected result: a `CNPY001-COUNT` finding and exit code `3` (`DO NOT
RELEASE`).

## 3. Detect a protected Account conflict

```bash
./bin/canopy-doctor check testdata/phase3/wrong-type
```

Expected result: a `CNPY003-NONCE-CONFLICT` finding for an incompatible
`types.Account.nonce` field 7 and exit code `3`.

## 4. Show conservative uncertainty

```bash
./bin/canopy-doctor check testdata/phase3/omitted
```

Expected result: `REVIEW REQUIRED`, not an asserted failure. Field omission
alone does not prove that an unknown protobuf field is lost.

## 5. Demonstrate immutable fork drift

CNPY004 requires a clean Git root, complete local history, and full commit SHAs.
The simplest repeatable demonstration is its isolated integration test:

```bash
go test -v ./internal/cli -run TestRunCNPY004HumanAndJSONAgreement
```

That test constructs temporary Git repositories and proves four outcomes:

| Scenario | Expected result |
| --- | --- |
| Documentation-only change with compatible active version | `PASS` / exit `0` |
| Exact compatibility-sensitive path changed | `REVIEW REQUIRED` / exit `2` |
| Deployment context omitted | `REVIEW REQUIRED` / exit `2` |
| Candidate version below an active requirement | `DO NOT RELEASE` / exit `3` |

For a real clean fork, run:

```bash
./bin/canopy-doctor check \
  --upstream-base 1111111111111111111111111111111111111111 \
  --upstream-target 2222222222222222222222222222222222222222 \
  --deployment-height 100000 \
  --activation-height 90000 \
  --required-protocol-version 2 \
  .
```

Replace the example SHAs and governance context with verified values. Canopy
Doctor never fetches them and does not infer live governance state.

## 6. Show CI output

```bash
./bin/canopy-doctor check --format json testdata/phase3/wrong-type
```

The JSON report uses schema version `1.0`, contains project-relative evidence
paths, and reaches the same decision as human output. See
`docs/OUTPUT_CONTRACT.md` for the stable shape and ordering.

## Interpretation

`PASS` only means no targeted violation was found under CNPY001-CNPY004 with
the supplied evidence. Continue normal code review, testing, upgrade rehearsal,
and validator coordination before release.
