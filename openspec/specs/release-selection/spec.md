# release-selection Specification

## Purpose

How baton decides which of a component's tags count as releases, how it
orders them, and which one is latest as of a date. Rollout triggers and
the `release_gomod` probe both use these rules.

## Requirements

### Requirement: Release sources

A component with `source: releases` (the default) SHALL read GitHub
releases. It SHALL skip drafts, date each release by its publish time,
and keep the forge's prerelease flag. A component with `source: tags`
SHALL read every Git tag and date each by its commit's committer date.
It SHALL make one commit lookup per tag, whether or not the tag counts.
Tags SHALL have no prerelease flag.

#### Scenario: Draft release

- **WHEN** a repository has a draft release
- **THEN** it never counts

#### Scenario: Tag dating

- **WHEN** a component reads tags
- **THEN** each tag's date is its commit's committer date, not when the
  tag was pushed

### Requirement: Tag commit

A rollout SHALL bind the commit the release's tag points to. For tag
sources, it SHALL use the commit from the tag listing. For GitHub
releases, it SHALL resolve `refs/tags/<tag>`, peeling annotated tags, so
a branch with the same name doesn't match. A failed lookup SHALL make
every obligation in the rollout an error.

#### Scenario: Tag lookup fails

- **WHEN** resolving the trigger tag's commit fails
- **THEN** each obligation of that rollout is `error`

### Requirement: As-of cutoff

A release SHALL count as of a time only if it was published at or before
that time.

#### Scenario: Before the first release

- **WHEN** `plan --as-of 2020-01-01` runs on the Scorecard example
- **THEN** the rollout is `not triggered` and each obligation is
  `not-applicable` with `no trigger release yet`

### Requirement: Counting filter

A release SHALL count only when:

- it matches every tag pattern that applies. Probes use the component's
  `releases.tag`. Triggers use the component's pattern and `when.tag`;
- it isn't a prerelease, unless `releases.prerelease` is true. A
  release is a prerelease when the forge flags it, or when its tag is a
  valid SemVer version with a prerelease suffix and no build metadata;
- under `version` order, its tag is a valid SemVer version with no
  build metadata;
- for triggers, it meets `when.level`.

#### Scenario: Prerelease by suffix

- **WHEN** a tag is `v1.2.0-rc.1` and the forge doesn't flag it
- **THEN** it doesn't count unless `prerelease: true`

#### Scenario: Prerelease with build metadata

- **WHEN** a tag is `v1.2.0-rc.1+b1`, the forge doesn't flag it, and
  the component uses `order: published`
- **THEN** it counts as a stable release

#### Scenario: Non-version tag under published order

- **WHEN** a component has `releases: {order: published}` and a tag
  `2026.02`
- **THEN** the tag counts

### Requirement: Ordering

Under `version` order, releases SHALL sort by SemVer, newest first.
Under `published` and `forge` order, they SHALL sort by publish date,
newest first. The latest release SHALL be the first in that order.

#### Scenario: Out-of-order publishing

- **WHEN** `v1.9.1` is published after `v2.0.0` under `version` order
- **THEN** `v2.0.0` is latest

### Requirement: Forge latest flag

Under `forge` order, the release GitHub marks as latest SHALL come
first, followed by the rest by publish date. The flag reflects only the
present, so a time more than one hour before now SHALL be unknowable.
Releases are listed before that test, so a listing failure in a replay
SHALL be an error, not unknown. If the repository has no latest release,
there SHALL be no releases. If the flagged release doesn't count under
the filter, the lookup SHALL fail.

#### Scenario: Replaying a forge-ordered trigger

- **WHEN** a rollout's trigger component uses `order: forge` and
  `plan --as-of 2026-06-01` runs
- **THEN** each obligation is `unknown` with
  `<repo> latest flag: can't be evaluated for a past date`
- **AND** the rollout line reads `not triggered`

#### Scenario: Forge order now

- **WHEN** the same rollout is evaluated as of now
- **THEN** it binds the flagged release

### Requirement: Trigger level

`when.level: minor` SHALL count only `vX.Y.0` releases, and
`when.level: major` only `vX.0.0`. A prerelease suffix SHALL be ignored
for this test. A level SHALL NOT change what counts as a release for
probes.

#### Scenario: Minor trigger skips patches

- **WHEN** `when: {release: scorecard, level: minor}` is replayed as of
  2026-06-01, after v5.5.0 and before v5.6.0
- **THEN** the rollout binds `scorecard@v5.5.0`

### Requirement: Probe source override

`release_gomod` SHALL accept a `source` key in its spec that overrides
the target component's `source` for that probe only.

#### Scenario: Tags for one probe

- **WHEN** a component reads GitHub releases and a `release_gomod` spec
  sets `source: tags`
- **THEN** that probe reads tags
