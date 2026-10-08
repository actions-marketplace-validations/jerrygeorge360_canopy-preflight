# Security Policy

Canopy Doctor inspects repositories that may be untrusted. Security reports are
welcome, especially for behavior that crosses the read-only inspection boundary,
executes target code, leaks machine-specific paths, or produces an unjustified
release decision.

## Reporting a vulnerability

Please report vulnerabilities privately through
[GitHub Security Advisories](https://github.com/jerrygeorge360/canopy-preflight/security/advisories/new).
Do not open a public issue containing exploit details, private repository data,
credentials, or other sensitive evidence.

Include, when possible:

- the affected Canopy Doctor version or commit SHA;
- the operating system and Git/Go versions;
- a minimal reproduction using non-sensitive files;
- the expected and observed behavior;
- the potential impact.

Please allow maintainers time to investigate before publishing details. A
maintainer will coordinate disclosure and credit with the reporter after the
issue and affected versions are understood.

## Inspection security boundary

Canopy Doctor treats the inspected project as untrusted data.

- It reads supported source, descriptor, and Git metadata.
- It does not run project binaries, tests, generators, build scripts, Git hooks,
  or plugin code.
- CNPY004 disables target-repository hooks, external diff commands, lazy object
  fetching, terminal prompts, and Git pagers for its own Git subprocesses.
- It does not modify the inspected project.
- It bounds filesystem traversal, source sizes, descriptor processing, Git
  output, and Git command duration.
- Reports use project-relative evidence paths and must not expose absolute local
  paths.

The Git executable itself is trusted infrastructure. Run Canopy Doctor in a
least-privileged CI job when inspecting contributions from untrusted authors.
Do not provide secrets that the check does not need.

## Decision boundary

`PASS` means that no incompatibility covered by the four implemented rules was
found with the supplied evidence. It is not a proof that a fork, plugin, or
upgrade is safe. Unsupported or incomplete evidence is reported as `REVIEW
REQUIRED` or as an operational error instead of being treated as safe.
