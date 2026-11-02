# obligation-evaluation Specification

## Purpose

How baton turns a baton file and what it observes into one state per
obligation: rollout instances, keys, prerequisites, clocks, grace,
waivers, invariants, and rollout completion.

## Requirements

### Requirement: One instance per rollout

Each rollout SHALL produce exactly one instance, bound to the latest
counted trigger release as of the time. Older releases SHALL NOT be
evaluated. The instance's binding SHALL be `<component>@<tag>`. Its key
SHALL be `<source repo>:release:<tag>:<commit>`. Its home SHALL be
`home.repo`, or the source repository when `home` is absent.

#### Scenario: Scorecard replay

- **WHEN** `plan --as-of 2026-10-20` runs on the Scorecard example
- **THEN** the rollout binds `scorecard@v5.6.0` with key
  `ossf/scorecard:release:v5.6.0:61e84743356275f6ffca23b1b2f2cb945e19eb96`

#### Scenario: No home

- **WHEN** the file has no `home`
- **THEN** the parent issue is planned in the trigger component's repo

### Requirement: Unbound rollouts

When a rollout has no trigger release yet, every obligation SHALL be
`not-applicable` with `no trigger release yet`. When the trigger lookup
is unknowable, every obligation SHALL be `unknown` with the reason. When
it fails, every obligation SHALL be `error` with the failure. In all
three cases the instance SHALL have an empty key and binding.

#### Scenario: API failure

- **WHEN** every GitHub call fails during `plan`
- **THEN** every rollout obligation is `error` with
  `releases of <repo>: list releases of <repo>: ...`
- **AND** the run exits 2

### Requirement: Obligation identity

A rollout obligation's id SHALL be `<rollout>/<id>`. Its key SHALL be
`<id>@<binding>:<commit>`. An invariant's binding SHALL be `always`, and
its key `<id>@always`. In an unbound rollout, the key SHALL be `<id>@`,
with no binding or commit.

#### Scenario: Moved tag

- **WHEN** a tag is moved to another commit
- **THEN** the rollout and each obligation get new keys

### Requirement: Action text

In a rollout, every `{version}` in an obligation's `action` SHALL be
replaced with the trigger tag, including any `v` prefix. Probe specs
SHALL NOT be templated. Invariant actions SHALL be used as written.

#### Scenario: Version in the action

- **WHEN** the trigger tag is `v5.6.0` and the action is
  `Bump github.com/ossf/scorecard/v5 to {version}`
- **THEN** the action reads
  `Bump github.com/ossf/scorecard/v5 to v5.6.0`

### Requirement: Component start date

When an obligation's target has `since`, and the trigger release was
published before midnight UTC on that date, the obligation SHALL be
`not-applicable` with `<target> didn't exist until <since>`, and its
probes SHALL NOT run. `since` SHALL NOT apply to invariants.

#### Scenario: Release before the component existed

- **WHEN** `scorecard-infra` has `since: 2026-08-05` and
  `plan --as-of 2025-10-05` binds a release from 2025
- **THEN** `infra-source-updated` is `not-applicable`

#### Scenario: Invariant before the component existed

- **WHEN** an invariant targets a component with `since` and is
  evaluated at a date when the repository had no commits
- **THEN** it is `error`, not `not-applicable`

### Requirement: Prerequisites

Before running its probes, an obligation SHALL settle each `needs` entry
in order:

- a `done` or `not-applicable` prerequisite lets it go on;
- a `waived` prerequisite is remembered;
- any other state makes it `blocked` with `needs <id> (<state>)`, and
  its probes don't run.

When nothing blocks and some prerequisite was waived, the obligation
SHALL be `waived` with `inherits the waiver on <id>`, without running
its probes.

#### Scenario: Blocked by an overdue bump

- **WHEN** `plan --as-of 2026-10-20` runs on the Scorecard example
- **THEN** `allstar-released` is `blocked` with
  `needs allstar-source-updated (overdue)`

#### Scenario: Prerequisite regresses after the release

- **WHEN** a release already ships the bound version, but the default
  branch later drops back below it
- **THEN** the bump is `overdue` and the release step is `blocked`

#### Scenario: Inherited waiver over a met step

- **WHEN** a waived prerequisite's dependent would pass its own probe
- **THEN** the dependent is still `waived`, with
  `inherits the waiver on <id>`

### Requirement: Combining checks

An obligation with several checks SHALL:

- be `error` as soon as one check fails, with only that check's message
  as evidence;
- be not done if any answered check is not done, even if another is
  unknowable;
- be `unknown` if every answered check is done and at least one is
  unknowable;
- be `done` only if every check answers done.

With more than one check, each check's evidence SHALL be prefixed with
its probe name, and the checks joined by a semicolon and a space.

#### Scenario: Error hides other evidence

- **WHEN** the first of two `file_lacks` checks passes and the second
  reads a missing file
- **THEN** the obligation is `error` with only
  `file_lacks: probe failed: ...: MISSING.md not found`

### Requirement: Clock start

A rollout obligation's clock SHALL start at the trigger release's
publish time. For an obligation with `needs`, it SHALL instead start at
the latest time a `done` prerequisite became done, if that's later. A
prerequisite that's waived or not applicable, or whose probes can't say
when it became done, SHALL count from the trigger time.

#### Scenario: Clock from a prerequisite

- **WHEN** a bump became done on 02-03 for a trigger published 02-01
- **THEN** the release step's grace runs from 02-03

### Requirement: Grace and age

An obligation that's not done SHALL be `pending` while the time since
its clock start is at most its grace (inclusive), and `overdue` after. A
missing grace SHALL be zero, so the obligation is overdue as soon as any
time passes.

An overdue rollout obligation SHALL report its age as whole days since
its clock start, rounded down: the time since the clock started, not the
time past grace.

#### Scenario: Grace boundary

- **WHEN** grace is `7d` and the trigger was published at 02-01 00:00
  UTC
- **THEN** the obligation is `pending` at 02-08 00:00:00 and `overdue`
  with age 7 at 02-08 00:00:01

#### Scenario: Age shown

- **WHEN** Scorecard v5.6.0 was published 2026-10-12T00:21Z and
  `allstar-source-updated` (grace `7d`) is replayed at 2026-10-20
- **THEN** it reads `overdue (8d)`

#### Scenario: No grace

- **WHEN** an obligation has no `grace` and isn't done one minute after
  the trigger
- **THEN** it is `overdue` with age 0

### Requirement: Invariants

Each invariant SHALL be evaluated once per run, with binding `always`
and no bound version. Invariants have no clock, so an unmet invariant
SHALL be `overdue` with no age, whatever its `grace`.

#### Scenario: Unmet invariant

- **WHEN** `site-install-image` finds the retired image reference
- **THEN** it is `overdue` with no age

### Requirement: Waivers

A waiver SHALL apply to an obligation when its `obligation` equals the
obligation's id and its `binding` is empty or equals the binding
(`always` for invariants). It SHALL stay active through the last second
of its `expires` date in UTC, and the first active match SHALL win.

Only `pending` or `overdue` obligations SHALL become `waived`; an error
is never waived. The evidence SHALL become
`waived by <approver> until <expires>: <reason> (<original evidence>)`,
and the age SHALL be dropped.

#### Scenario: Expiry day

- **WHEN** a waiver expires 2026-02-16
- **THEN** the obligation is `waived` at 2026-02-16T23:59:59Z and
  `overdue` at 2026-02-17T00:00:00Z

#### Scenario: Invariant waiver

- **WHEN** a waiver for an invariant sets `binding: always`
- **THEN** it applies

### Requirement: Completion

A rollout instance SHALL be complete when every obligation is `done`,
`waived`, or `not-applicable`. An unbound rollout SHALL NOT be complete.

#### Scenario: Waived chain completes

- **WHEN** the only unmet step is waived and its dependents inherit the
  waiver
- **THEN** the rollout is complete
