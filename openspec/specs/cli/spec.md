# cli Specification

## Purpose

The `baton` command line: its commands, arguments, flags, credential
lookup, output formats, logging, and exit status. This records the
behavior of the preview (pre-1.0) tool as observed.

## Requirements

### Requirement: Command set

The `baton` binary SHALL provide the commands `validate`, `plan`,
`apply`, and `version`, plus cobra's `help` and `completion`. Run with
no command, it SHALL print help and exit 0.

#### Scenario: No command

- **WHEN** a user runs `baton` with no arguments
- **THEN** it prints usage listing `apply`, `completion`, `help`,
  `plan`, `validate`, and `version`
- **AND** exits 0

#### Scenario: Unknown command or flag

- **WHEN** a user runs `baton frobnicate` or
  `baton validate --nope x.yml`
- **THEN** it logs `unknown command` or `unknown flag` and exits 1

### Requirement: One file argument

`validate`, `plan`, and `apply` SHALL each take exactly one positional
argument, the path to a baton file. Any other count SHALL be a usage
error with exit status 1.

#### Scenario: Missing file argument

- **WHEN** a user runs `baton validate`
- **THEN** it logs `baton: accepts 1 arg(s), received 0` and exits 1

#### Scenario: Two file arguments

- **WHEN** a user runs `baton validate a.yml b.yml`
- **THEN** it logs `accepts 1 arg(s), received 2` and exits 1

### Requirement: Exit status

baton SHALL exit 0 on success. It SHALL exit 1 for a usage, flag,
logging, file, configuration, or credential error. It SHALL exit 2 when
a run completes but an obligation is in the error state, the inventory
check fails, or an issue write fails.

#### Scenario: Configuration error

- **WHEN** the baton file fails validation
- **THEN** every command that loads it exits 1

#### Scenario: Obligation error

- **WHEN** `plan` evaluates a file and any obligation's state is
  `error`
- **THEN** it prints the full report
- **AND** logs
  `evaluation failed: one or more obligations couldn't be evaluated` and
  exits 2

### Requirement: Error logging

baton SHALL report a failing command's error through logrus on standard
error, prefixed `baton:` and a space. When standard error isn't a
terminal, the error SHALL be one logfmt line. Several joined errors
appear in that line, separated by escaped `\n` sequences. Errors raised
before the logger is configured (argument count, unknown command) carry
a `time=` field; errors raised after don't. On a terminal, logrus SHALL
use its colored form, with an abbreviated level, instead.

#### Scenario: Error after logger setup

- **WHEN** `baton plan` fails for lack of a token, with standard error
  redirected
- **THEN** standard error holds
  `level=error msg="baton: github token: ..."` with no `time=` field

#### Scenario: Error before logger setup

- **WHEN** `baton validate` is run with no file
- **THEN** standard error holds a line starting
  `time="..." level=error`

### Requirement: Log level flag

The persistent `--log-level` flag SHALL accept the logrus levels
`panic`, `fatal`, `error`, `warning` (or `warn`), `info`, `debug`, and
`trace`, in any case, defaulting to `info`. Any other value SHALL fail
with exit status 1 after argument checks.

#### Scenario: Bad level

- **WHEN** a user runs `baton --log-level bogus validate baton.yml`
- **THEN** it logs `set up logging: setting log level to bogus` and
  exits 1

### Requirement: validate stays offline

`validate FILE` SHALL load and validate the file without any credential
or network access, and on success SHALL print
`<FILE>: <n> rollout(s), <n> invariant(s), <n> waiver(s): ok` and exit 0.

#### Scenario: Valid file

- **WHEN** a user runs `baton validate examples/scorecard/baton.yml`
  with no token and no `gh` on `PATH`
- **THEN** it prints
  `examples/scorecard/baton.yml: 1 rollout(s), 2 invariant(s), 0 waiver(s): ok`
- **AND** exits 0

#### Scenario: Missing file

- **WHEN** the file doesn't exist
- **THEN** it logs
  `load config: read config: open <FILE>: no such file or directory` and
  exits 1

### Requirement: Credential lookup

`plan` and `apply` SHALL take a GitHub token from `GITHUB_TOKEN`, else
`GH_TOKEN`, else the output of `gh auth token`. The `gh` fallback SHALL
NOT pin a host, so it returns the token for `gh`'s current host,
including one chosen by `GH_HOST`. Without a token, the command SHALL
exit 1.

#### Scenario: No token available

- **WHEN** neither variable is set and `gh` isn't on `PATH`
- **THEN** `plan` logs
  `github token: no GitHub token: set GITHUB_TOKEN or GH_TOKEN, or log in with gh`
  and exits 1

#### Scenario: GH_HOST changes the fallback token

- **WHEN** no token variable is set and `GH_HOST` names another GitHub
  host
- **THEN** baton uses that host's token for its calls to github.com

### Requirement: Order of plan checks

`plan` SHALL load and validate the file first, then obtain the token,
then parse `--as-of`. The first failure SHALL decide the error reported.

#### Scenario: Bad file without a token

- **WHEN** the file is invalid and no token is available
- **THEN** `plan` reports the configuration error, not the token error

#### Scenario: Bad date without a token

- **WHEN** `--as-of 2026-13-01` is given and no token is available
- **THEN** `plan` reports the token error, not the date error

### Requirement: As-of date

`plan --as-of YYYY-MM-DD` SHALL evaluate as of the last second of that
day in UTC, or as of now when that moment is later than now. Without the
flag, it SHALL evaluate as of now. A malformed date SHALL fail with exit
status 1. `apply` SHALL always evaluate as of now.

#### Scenario: Past date

- **WHEN** a user runs `baton plan --as-of 2026-10-20 baton.yml`
- **THEN** the report is as of `2026-10-20T23:59:59Z`

#### Scenario: Future date

- **WHEN** a user runs `baton plan --as-of 2099-01-01 baton.yml`
- **THEN** the report is as of now

#### Scenario: Malformed date

- **WHEN** `--as-of 2026-13-01` is given with a valid token
- **THEN** it logs `--as-of: parse date` and exits 1

### Requirement: Text report

Without `--json`, `plan` and `apply` SHALL print:

- `as of <date>`;
- per rollout, `rollout <id> <binding>: <status>`, where status is
  `complete`, `in progress`, or `not triggered`;
- per obligation, state, age as `(<n>d)` if any, ID, and evidence;
- `invariants` followed by one line per invariant, when there are any;
- per plan item, `plan: <op> <repo> [<key>] <title>`.

#### Scenario: Replay output

- **WHEN** a user runs `baton plan --as-of 2026-10-20` on the Scorecard
  example
- **THEN** the output includes
  `rollout scorecard-release scorecard@v5.6.0: in progress`
- **AND** an obligation line with state `overdue`, age `(8d)`, and ID
  `scorecard-release/allstar-source-updated`

### Requirement: Text report layout

Rollout, `invariants`, and plan lines SHALL be indented two spaces, and
obligation lines four. Obligation lines SHALL pad the state to 14
columns, the age to 6, and the ID to 42. Plan lines SHALL pad the
operation to 14 columns and the repository to 22.

#### Scenario: Aligned columns

- **WHEN** a report has `done` and `overdue (8d)` obligations
- **THEN** their IDs start in the same column

### Requirement: Unbound rollout status

Any rollout without a bound release SHALL print as `not triggered`,
including one whose trigger lookup was unknown or failed. Its binding is
then empty, which leaves a space before the colon.

#### Scenario: Failed trigger lookup

- **WHEN** every GitHub call fails during `plan`
- **THEN** the rollout line reads
  `rollout scorecard-release : not triggered` and each obligation is
  `error`

### Requirement: Report date

The `as of` date SHALL be in UTC when `--as-of` names a past date. When
evaluating as of now, including an `--as-of` of today or later, it SHALL
be in the local time zone.

#### Scenario: Local date when evaluating now

- **WHEN** `plan` runs without `--as-of` with `TZ` behind UTC, so the
  local date is the day before the UTC date
- **THEN** the header shows the local date

### Requirement: JSON report

With `--json`, `plan` and `apply` SHALL print one indented JSON object
with `report` (as-of time, rules digest, rollouts with their
obligations, invariants) and `plan` (the issue changes). HTML characters
SHALL be escaped, so issue markers appear as `\u003c!-- ... --\u003e`.
`apply` SHALL then print its plain-text `apply:` lines to the same
output, so `apply --json` output isn't a single JSON document.

#### Scenario: JSON replay

- **WHEN** a user runs `baton plan --json --as-of 2026-10-20` on the
  Scorecard example
- **THEN** the output has `"as_of": "2026-10-20T23:59:59Z"` and an
  obligation with `"age_days": 8`

### Requirement: apply needs a redirect owner

`apply` SHALL refuse to run without `--redirect-owner`, before reading
the file or obtaining a token. It SHALL exit 1 with
`apply needs --redirect-owner: the preview doesn't write to real repos`.

#### Scenario: No redirect owner

- **WHEN** a user runs `baton apply does-not-exist.yml`
- **THEN** it reports the missing `--redirect-owner`, not the missing
  file
- **AND** exits 1

### Requirement: apply flow

`apply` SHALL evaluate as of now and print the report and plan as `plan`
does. It SHALL then carry out the plan and print one `apply: <action>`,
indented two spaces, line per write. It SHALL carry out the plan even
when evaluation produced errors, and SHALL then exit 2. `--create-label`
SHALL create a missing `baton` label instead of failing.

#### Scenario: Evaluation error during apply

- **WHEN** one invariant errors and a rollout obligation is pending
- **THEN** `apply` still opens the pending obligation's issue
- **AND** exits 2

### Requirement: version

`version` SHALL print release-utils build information (an ASCII-art
banner, `GitVersion`, `GitCommit`, `GitTreeState`, `BuildDate`,
`GoVersion`, `Compiler`, `Platform`) and SHALL accept `--json`.

#### Scenario: Version JSON

- **WHEN** a user runs `baton version --json`
- **THEN** the output contains a `gitVersion` field
