# GitHub Action

Canopy Doctor can run as a pull-request compatibility gate. The action builds
the checked repository version of the CLI and inspects the selected project
path.

Use the full commit SHA for a reviewed release. The example below deliberately
contains `<full-release-commit-sha>` as a placeholder; replace it before
committing the workflow. A version tag is easier to read but can move unless the
repository enforces immutable releases or tag protection.

## Plugin and schema checks

```yaml
name: Canopy Doctor

on:
  pull_request:
  push:

permissions:
  contents: read

jobs:
  compatibility:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@11d5960a326750d5838078e36cf38b85af677262 # v4
      - name: Check Canopy compatibility
        uses: jerrygeorge360/canopy-preflight@<full-release-commit-sha>
        with:
          path: .
          format: human
```

The job follows the CLI exit contract. A review finding exits `2`, and a proven
blocking finding exits `3`; both fail a normal GitHub Actions step. An input or
operational failure exits `1`.

## CNPY004 fork-drift check

CNPY004 compares commits already present in the local Git object database. It
does not fetch missing history. Checkout must therefore use `fetch-depth: 0`.

```yaml
name: Canopy Fork Compatibility

on:
  workflow_dispatch:
    inputs:
      upstream_base:
        description: Full 40-character comparison base SHA
        required: true
      upstream_target:
        description: Full 40-character comparison target SHA
        required: true
      deployment_height:
        description: Planned deployment height
        required: true
      activation_height:
        description: Required-version activation height
        required: true
      required_protocol_version:
        description: Required protocol version
        required: true

permissions:
  contents: read

jobs:
  compatibility:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@11d5960a326750d5838078e36cf38b85af677262 # v4
        with:
          fetch-depth: 0
      - name: Check immutable fork comparison
        uses: jerrygeorge360/canopy-preflight@<full-release-commit-sha>
        with:
          path: .
          format: json
          upstream-base: ${{ inputs.upstream_base }}
          upstream-target: ${{ inputs.upstream_target }}
          deployment-height: ${{ inputs.deployment_height }}
          activation-height: ${{ inputs.activation_height }}
          required-protocol-version: ${{ inputs.required_protocol_version }}
```

Both upstream SHA inputs must be supplied together. The three deployment-context
inputs must likewise be supplied together or all omitted. Omitting deployment
context produces `REVIEW REQUIRED`, because Canopy Doctor cannot determine
whether a required protocol version is active.

The inspected candidate is the clean committed `HEAD` at the project root. The
base must be an ancestor of both the target and candidate. Symbolic refs, short
SHAs, dirty worktrees, shallow history, and missing objects are rejected as
input errors.

## Inputs

| Input | Default | Requirement |
| --- | --- | --- |
| `path` | `.` | Project directory; CNPY004 requires the exact Git worktree root |
| `format` | `human` | `human` or `json` |
| `upstream-base` | empty | Full 40-character SHA; pair with `upstream-target` |
| `upstream-target` | empty | Full 40-character SHA; pair with `upstream-base` |
| `deployment-height` | empty | Unsigned decimal; supply the complete context trio |
| `activation-height` | empty | Unsigned decimal; supply the complete context trio |
| `required-protocol-version` | empty | Unsigned decimal; supply the complete context trio |

## Security notes

The action reads the checked-out project but does not execute its binaries,
tests, generators, hooks, or plugins. Keep workflow permissions read-only and do
not expose unnecessary secrets to a job that inspects untrusted pull requests.
See `SECURITY.md` for the complete boundary.
