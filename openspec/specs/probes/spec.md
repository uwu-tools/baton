# probes Specification

## Purpose

The checks that decide whether an owed action is done, as of a date:
`gomod`, `release_gomod`, `deployed_gomod`, `files_equal`, and
`file_lacks`. Each returns done or not, evidence text, and, where it can
tell, when the fact became true.

## Requirements

### Requirement: Outcomes

A probe SHALL return one of three outcomes:

- an answer: done or not done, with evidence;
- unknowable for the requested time, which isn't a failure;
- a failure, which makes the obligation an error and is never reported
  as not done.

#### Scenario: Missing file

- **WHEN** a probe reads a file that doesn't exist at the ref
- **THEN** the obligation is `error`, not pending or overdue

### Requirement: Failure wording

Failures found in file contents or probe specs (a missing or unparsable
file, a missing requirement, an uncomparable version, a missing field)
SHALL be worded `probe failed: ...`. Other failures SHALL keep their own
wording. Examples are `not found: ...` for a missing commit,
`unexpected HTTP status: ...`, `not a JSON object: ...`, and API errors.

#### Scenario: HTTP failure

- **WHEN** a `deployed_gomod` endpoint returns 503
- **THEN** the evidence includes `unexpected HTTP status: 503`, without
  `probe failed`

### Requirement: Default-branch commit

Probes that read a repository "as of" a time SHALL use the newest
default-branch commit at or before that time. If there is none, the
probe SHALL fail with
`<repo> has no default-branch commit before <time>`.

#### Scenario: Repository too young

- **WHEN** an invariant is replayed as of 2020-01-01 against
  `ossf/scorecard`
- **THEN** it errors with
  `ossf/scorecard has no default-branch commit before 2020-01-01T23:59:59Z`

### Requirement: Version comparison

Version-bound probes SHALL compare the version a `go.mod` requires with
the bound version using Go module SemVer, and SHALL be done when the
required version is at or above it. A pseudo-version for a later commit
SHALL count as newer than the release it builds on. If either version
isn't valid SemVer, the probe SHALL fail with
`can't compare versions "<have>" and "<want>"`. The bound version SHALL
be the trigger's tag; an invariant binds none, so it compares against
`""`.

#### Scenario: Non-SemVer trigger tag

- **WHEN** a trigger component uses `order: published` and its latest
  tag is `2026.02`
- **THEN** a `gomod` obligation errors with
  `can't compare versions "v1.1.0" and "2026.02"`

#### Scenario: Version probe in an invariant

- **WHEN** an invariant verifies with `gomod`
- **THEN** it errors with `can't compare versions "<have>" and ""`

### Requirement: Reading go.mod

Version-bound probes SHALL read `path` (default `go.mod`) at a ref,
parse it leniently, and find the `require` entry for `module`. A missing
file SHALL fail with `no <path> at <ref>`. A file that doesn't parse, or
doesn't require the module, SHALL fail. An empty or missing `module` key
SHALL fail the same way, naming an empty module.

#### Scenario: Wrong key for the module

- **WHEN** the spec is `{gomod: {mod: github.com/ossf/scorecard/v5}}`
- **THEN** the obligation errors with a message ending in
  `go.mod doesn't require` and no module name

### Requirement: gomod

`gomod` SHALL check the target's default branch as of the time, with
evidence `<repo>@<12-char sha> requires <version>`.

#### Scenario: Bumped on main

- **WHEN** `plan --as-of 2026-10-20` runs on the Scorecard example
- **THEN** `action-source-updated` is `done` with
  `ossf/scorecard-action@365df9f035d4 requires v5.6.0`

### Requirement: gomod done time

When `gomod` is done, it SHALL report when the fact became true: the
committer date of the oldest commit in the unbroken run of satisfying
commits. It walks back through up to 100 commits that touched `path`,
newest first, and stops at the first that doesn't satisfy the bound
version. A `go.mod` that doesn't parse or doesn't require the module
SHALL end the walk; a failed read SHALL fail the probe.

#### Scenario: Run of bumps

- **WHEN** `go.mod` reached the bound version on 02-03 and every later
  `go.mod` commit kept it
- **THEN** the obligation became done on 02-03

### Requirement: release_gomod

`release_gomod` SHALL take the latest release of the target component as
of the time, using the component's release rules. It SHALL NOT apply the
trigger's narrowing. It SHALL read `path` at that release's tag. With no
release yet, it SHALL be not done with `<repo>: no release yet`. Its
evidence SHALL be `<repo> <tag> requires <version>`. When done, it SHALL
report the publish date of the oldest release in the unbroken run of
satisfying releases, walking down from the latest.

#### Scenario: Released

- **WHEN** `plan --as-of 2026-10-20` runs on the Scorecard example
- **THEN** `action-released` is `done` with
  `ossf/scorecard-action v2.5.0 requires v5.6.0`

### Requirement: deployed_gomod

`deployed_gomod` SHALL be unknowable for any time more than one hour
before now. Otherwise it SHALL:

- GET `url` and decode a JSON object, failing on a non-200 status or a
  body that isn't an object;
- read the commit from `field` (default `commit`), failing unless it's
  a non-empty string;
- read `path` at that commit in the target repo, with evidence
  `deployed <12-char sha> requires <version>`.

It SHALL NOT report when the fact became true.

#### Scenario: Replay

- **WHEN** `plan --as-of 2026-10-20` runs on the Scorecard example
- **THEN** `infra-deployed` is `unknown` with
  `can't be evaluated for a past date`

#### Scenario: Live

- **WHEN** `plan` runs as of now and the endpoint reports a commit
  whose `go.mod` requires v5.6.0
- **THEN** `infra-deployed` is `done` with
  `deployed <sha> requires v5.6.0`

### Requirement: files_equal

`files_equal` SHALL take `source` and `copy`, each `{component, path}`.
It SHALL read each file at its own repository's default-branch commit as
of the time. It SHALL be done when the two contents are byte-for-byte
identical (`identical`), and not done otherwise (`contents differ`). A
missing side, an unknown component, or a missing file SHALL fail the
probe. These are checked at evaluation, not by `validate`.

#### Scenario: Copied schema

- **WHEN** `plan` runs on the Scorecard example
- **THEN** `infra-schema-copy` is `done` with `identical`

### Requirement: file_lacks

`file_lacks` SHALL read `path` in the target repository as of the time
and check each line for `text`. If any line contains it, the probe SHALL
be not done, with `<path> line <n>[,<n>...] contains "<text>"`.
Otherwise it SHALL be done, with `<path> doesn't contain "<text>"`. An
empty `text` or a missing file SHALL fail the probe. Because the match
is per line, a `text` containing a newline SHALL never match.

#### Scenario: Retired image still referenced

- **WHEN** `plan` runs on the Scorecard example
- **THEN** `site-install-image` is `overdue` with
  `scorecards-site/content/home.md line 105 contains "gcr.io/openssf/scorecard:stable"`
