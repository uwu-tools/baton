# inventory-check Specification

## Purpose

The check that every component in the baton file is listed in the
project's Security Insights file. `plan` and `apply` run it before
evaluating obligations.

## Requirements

### Requirement: When it runs

When the file has `inventory`, `plan` and `apply` SHALL read
`inventory.path` from the inventory component's repository at its
current default-branch commit. They SHALL read the current commit even
under `--as-of`. Without `inventory`, no check SHALL run.

#### Scenario: Replay reads today's file

- **WHEN** `plan --as-of 2020-01-01` runs on the Scorecard example
- **THEN** the inventory check reads the present-day
  `security-insights.yml` and passes

### Requirement: Matching

The check SHALL parse the file as YAML and collect
`project.repositories[].url`. From each URL it SHALL strip one trailing
`/` and then the prefix `https://github.com/`. Every component's `repo`
SHALL match one of the results exactly. Matching SHALL be
case-sensitive, and other URL forms, such as a `.git` suffix or
`http://`, SHALL NOT match.

#### Scenario: Trailing slash

- **WHEN** the file lists `https://github.com/ex/core/`
- **THEN** component repo `ex/core` matches

#### Scenario: .git suffix and case

- **WHEN** the file lists `https://github.com/ex/action.git` and
  `https://github.com/EX/late`
- **THEN** components `ex/action` and `ex/late` are reported missing

### Requirement: Failures

A missing component SHALL fail the check with
`inventory check failed: not listed in <path>: <name> (<repo>), ...`. A
missing file, a file with no repositories, or a parse error SHALL also
fail it. So SHALL an API error, worded `inventory: ...`. A failure SHALL
be logged at once. Evaluation and the report SHALL still go ahead, and
the run SHALL exit 2.

#### Scenario: Unlisted component

- **WHEN** a component `uwu-tools/baton` isn't in Scorecard's
  `security-insights.yml`
- **THEN** `plan` logs
  `inventory check failed: not listed in security-insights.yml: baton (uwu-tools/baton)`
  before the report
- **AND** prints the report and exits 2
