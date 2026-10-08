# Upstream monitoring

Canopy Preflight checks the official `canopy-network/canopy` repository every
day. The workflow compares `main`, `development`, and the latest release with
the newest upstream commit that has completed a compatibility review.

The monitor does not change rules or approve new Canopy behavior. When a new
release appears, history diverges, or a monitored file changes, it opens or
updates one issue named `Canopy upstream compatibility review required`.

## Baseline

[`upstream-baseline.json`](upstream-baseline.json) contains the reviewed commit,
reviewed release, and watched branches. A new upstream commit is not considered
reviewed merely because CI passes.

Update the baseline only after:

1. reading the upstream diff;
2. confirming whether CNPY001-CNPY004 still describe current Canopy behavior;
3. updating rule contracts and fixtures where required;
4. running `make check`;
5. testing Canopy Doctor against the official Canopy plugin; and
6. receiving pull-request review.

## Watched surfaces

The monitor maps changes in the plugin handshake, codec, protobuf definitions,
state-prefix guards, protocol activation, governance, transaction handling, and
official plugin contracts or tutorials to the affected CNPY rules.

Ordinary UI, documentation, and implementation changes outside those surfaces
do not create compatibility alerts.

## Manual run

Open **Actions → Canopy upstream watch → Run workflow**. The job writes the full
report to its workflow summary. If action is required, it creates or updates the
single rolling review issue instead of opening duplicates.

The scheduled job runs at 05:17 UTC each day. A workflow failure means the
monitor could not complete and must not be treated as confirmation that no
upstream change occurred.
