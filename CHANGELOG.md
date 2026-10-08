# Changelog

This project follows [Semantic Versioning](https://semver.org/).

## Unreleased

- Added `canopy-doctor version`, `canopy-doctor --version`, and `-v`.
- Simplified the repository and product documentation.
- Removed development-only orchestration files from the product tree.

## v0.2.0 - 2026-09-29

- Added support for the official Canopy Go `ContractConfig` pointer flow.
- Added strict structural verification for the official configuration path.
- Kept modified, aliased, or ambiguous flows at `REVIEW REQUIRED`.
- Verified the checker against the official Canopy Go plugin.

## v0.1.1 - 2026-09-17

- Prevented a panic when descriptor initialization contains nil AST expressions.
- Added a regression test for the official Canopy plugin layout.

## v0.1.0 - 2026-09-15

- Initial private release with CNPY001 through CNPY004.
- Added human and JSON reports, fixtures, CI, and six platform binaries.
