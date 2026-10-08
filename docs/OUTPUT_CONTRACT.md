# Output Contract

## Version

The initial machine-readable schema version is `1.0`. Additive changes require a
minor version; removal, renaming, type changes, or semantic changes require a
major version.

Reports contain no timestamps, absolute machine paths, or nondeterministically
ordered values.

## Decisions and process exits

| Outcome | JSON value | Exit code |
| --- | --- | ---: |
| No supported finding | `PASS` | 0 |
| Operational or input error | no report decision | 1 |
| Human review needed | `REVIEW REQUIRED` | 2 |
| Proven blocking violation | `DO NOT RELEASE` | 3 |

Aggregation precedence is operational error, `DO NOT RELEASE`,
`REVIEW REQUIRED`, then `PASS`.

## JSON shape

```json
{
  "schema_version": "1.0",
  "tool": {
    "name": "canopy-doctor",
    "version": "dev"
  },
  "target": ".",
  "decision": "PASS",
  "findings": []
}
```

Each finding contains:

```json
{
  "rule_id": "CNPY001",
  "code": "CNPY001-COUNT",
  "severity": "high",
  "decision": "DO NOT RELEASE",
  "summary": "Transaction declaration counts differ",
  "details": "Observed two transaction names and one type URL.",
  "evidence_locations": [
    {
      "path": "plugin/config.go",
      "line": 42,
      "detail": "supported_transactions"
    }
  ],
  "remediation": "Make both ordered lists the same length.",
  "confidence": "high"
}
```

`findings` and `evidence_locations` are arrays, never `null`. Paths are
slash-separated and relative to the inspected project. A line of `0` means the
finding applies to the file or project rather than one source line.

## Deterministic ordering

Findings are ordered by:

1. `rule_id`;
2. `code`;
3. first evidence path;
4. first evidence line;
5. `summary`.

Evidence locations within a finding are ordered by path, line, then detail.

## Human output

Human output uses the same normalized report and decision as JSON. It contains
the tool name, target, final decision, finding count, and complete finding
evidence and remediation. It contains no color or terminal-dependent formatting,
so golden output remains stable.
