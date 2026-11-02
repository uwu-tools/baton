# config Specification

## Purpose

The baton file (`baton.yml`): its schema, how it's parsed, the checks
`validate` applies, and the rules digest. Every command that reads a
baton file loads it through these rules.

## Requirements

### Requirement: File schema

A baton file SHALL be YAML with these top-level keys, all optional:

- `home`: `{repo}`, the repository for parent rollout issues;
- `inventory`: `{component, path}`, the Security Insights file to
  check;
- `components`: a map from name to component;
- `rollouts`: a list of `{id, when, obligations}`;
- `invariants`: a list of obligations;
- `waivers`: a list of waivers.

#### Scenario: Empty file

- **WHEN** the file is empty
- **THEN** `validate` reports
  `0 rollout(s), 0 invariant(s), 0 waiver(s): ok`

### Requirement: Component, trigger, and waiver shapes

A component SHALL be `{repo, source, releases, since, owners}`, where
`releases` is `{tag, prerelease, order}`. A rollout's `when` SHALL be
`{release, level, tag}`. A waiver SHALL be
`{obligation, binding, approver, reason, expires}`.

#### Scenario: Full component

- **WHEN** a component sets all five keys and all three `releases` keys
- **THEN** it loads

### Requirement: Obligation shape

An obligation SHALL be
`{id, type, target, needs, action, verify, grace}`. `verify` SHALL take
one check as a mapping or several as a list. Each check is a mapping
from a probe name to that probe's spec.

#### Scenario: Check list

- **WHEN** `verify` is a list of two single-probe mappings
- **THEN** both checks are loaded and must all pass

### Requirement: Strict keys

Loading SHALL reject any key the schema doesn't define, at any level
except inside a probe's spec. The error text SHALL be the YAML-to-JSON
decoder's, for example
`error unmarshaling JSON: while decoding JSON: json: unknown field "obligatons"`.

#### Scenario: Misspelled key

- **WHEN** a rollout has `obligatons:` instead of `obligations:`
- **THEN** loading fails with `unknown field "obligatons"` and exit 1

### Requirement: All errors at once

Validation SHALL collect every problem and report them together, each
prefixed `invalid config:`. Errors for components SHALL appear in map
iteration order, which varies from run to run.

#### Scenario: Several problems

- **WHEN** a file has a component without `repo` and an obligation
  without `action`
- **THEN** one run reports both

#### Scenario: Unstable order

- **WHEN** five components each lack `repo`
- **THEN** repeated runs list them in different orders

### Requirement: Component checks

Validation SHALL check each component:

- `repo` is present;
- `source` is `releases` (the default) or `tags`;
- `since` is a `YYYY-MM-DD` date;
- `releases.tag` compiles as an RE2 pattern;
- `releases.order` is `version` (the default), `published`, or `forge`,
  and `forge` requires `source: releases`.

Validation SHALL NOT check `owners` or `repo` syntax, and SHALL allow
two components with the same repo.

#### Scenario: Forge order on tags

- **WHEN** a component has `source: tags` and
  `releases: {order: forge}`
- **THEN** validation fails with
  `releases.order forge needs source releases; tags have no latest flag`

#### Scenario: Shared repo

- **WHEN** two components name the same `repo`
- **THEN** validation passes

### Requirement: Home and inventory checks

When `home` is present, it SHALL have `repo`. When `inventory` is
present, its `component` SHALL name a component. Validation SHALL NOT
require `inventory.path`.

#### Scenario: Unknown inventory component

- **WHEN** `inventory.component` is `zzz` and no such component exists
- **THEN** validation fails with `inventory: unknown component "zzz"`

### Requirement: Rollout checks

Validation SHALL check each rollout:

- rollout IDs are unique, and obligation IDs are unique within a
  rollout;
- `when.release` names a component and `when.tag` compiles;
- `when.level` is `major`, `minor`, or `patch` (the default), and
  `major` or `minor` is rejected when the source component sets
  `releases.order` to anything but `version`;
- each `needs` entry names an obligation in the same rollout, and the
  `needs` graph has no cycle.

#### Scenario: Needs cycle

- **WHEN** `a` needs `b` and `b` needs `a`
- **THEN** validation fails with `needs cycle through "<id>"`, naming
  one node on the cycle, which can differ from run to run

#### Scenario: Level with published order

- **WHEN** `when.level: minor` and the source component has
  `releases: {order: published}`
- **THEN** validation fails with
  `when.level needs the <component> component's releases ordered by version`

#### Scenario: Level with default order

- **WHEN** `when.level: minor` and the source component sets no order
- **THEN** validation passes

### Requirement: IDs and obligations optional

Validation SHALL NOT require non-empty IDs, or a rollout to have any
obligations.

#### Scenario: Empty IDs

- **WHEN** a rollout and its obligation both have `id: ""`
- **THEN** validation passes

#### Scenario: No obligations

- **WHEN** a rollout has `obligations: []`
- **THEN** validation passes

### Requirement: Obligation checks

Validation SHALL check each obligation:

- `target` names a component and `action` is non-empty;
- `verify` holds at least one check, each naming exactly one probe that
  baton knows (`gomod`, `release_gomod`, `deployed_gomod`,
  `files_equal`, `file_lacks`);
- `grace` parses as described below;
- invariants have no `needs`.

Validation SHALL NOT check the keys or values inside a probe's spec, or
whether a probe needs a version that an invariant can't supply.

#### Scenario: Two probes in one check

- **WHEN** a check is `{gomodd: {}, gomod: {module: m}}`
- **THEN** validation reports
  `verify check 1 must name exactly one probe` and
  `unknown probe "gomodd"`

#### Scenario: Wrong key in a probe spec

- **WHEN** a check is `{gomod: {mod: github.com/ex/core}}`
- **THEN** validation passes, and the probe fails at evaluation

#### Scenario: Version probe in an invariant

- **WHEN** an invariant verifies with `gomod`
- **THEN** validation passes, and the invariant errors at evaluation

### Requirement: Grace format

A grace period SHALL be empty (meaning zero) or a non-negative integer
followed by `d`, as parsed by Go's `strconv.Atoi`. A leading `+` SHALL
therefore be accepted.

#### Scenario: Accepted forms

- **WHEN** grace is `7d`, `0d`, `+7d`, or omitted
- **THEN** validation passes

#### Scenario: Missing unit

- **WHEN** grace is `7`
- **THEN** validation fails with `grace must look like "7d", got "7"`

### Requirement: Invariant and waiver references

Invariant IDs SHALL be unique and SHALL NOT equal any
`<rollout>/<obligation>` ID. Each waiver's `obligation` SHALL name a
known `<rollout>/<obligation>` or invariant ID. `approver` and `reason`
SHALL both be non-empty, and `expires` SHALL be a `YYYY-MM-DD` date.
Validation SHALL NOT reject an expired waiver or check `binding`.

#### Scenario: Invariant ID collides

- **WHEN** rollout `r` has obligation `a` and an invariant has ID `r/a`
- **THEN** validation fails with `invariant "r/a": duplicate id`

#### Scenario: Expired waiver

- **WHEN** a waiver expires on 2000-01-01
- **THEN** validation passes

### Requirement: Rules digest

Loading SHALL set the rules digest to the first 12 hex digits of the
SHA-256 of the file's bytes. Any byte change, including a comment, SHALL
change the digest.

#### Scenario: Digest in markers

- **WHEN** a plan is made from a loaded file
- **THEN** each issue marker includes `rules=<12 hex digits>`
