# issue-planning Specification

## Purpose

How baton maps an evaluation report to a list of issue changes: which
issues to open, update, or close, in which repository, with what title,
body, hidden marker, and assignees. `plan` prints this list and `apply`
carries it out.

## Requirements

### Requirement: Plan operations

Each plan item SHALL carry one operation:

- `open-or-update`: create the issue, or reopen and update it;
- `update-if-open`: update the issue if it's open, never open one;
- `close-if-open`: close the issue as completed, with evidence.

#### Scenario: Operation names in output

- **WHEN** `plan --as-of 2026-10-20` runs on the Scorecard example
- **THEN** its plan lines use `open-or-update`, `update-if-open`, and
  `close-if-open`

### Requirement: Parent items

Each bound rollout instance SHALL yield one parent item in its home
repository, keyed by the instance key. An incomplete instance SHALL be
`open-or-update` with title `[rollout] <binding>`, with the first `@`
replaced by a space, and a checklist body. A complete instance SHALL be
`close-if-open` with evidence `rollout complete` and no title or body.
An unbound rollout SHALL yield no items at all.

#### Scenario: Parent title

- **WHEN** the binding is `scorecard@v5.6.0`
- **THEN** the parent title is `[rollout] scorecard v5.6.0`

#### Scenario: Complete rollout

- **WHEN** `plan --as-of 2025-10-05` runs on the Scorecard example
- **THEN** the parent item is `close-if-open`

#### Scenario: Unbound rollout

- **WHEN** a rollout's trigger is unknown or failed
- **THEN** the plan has no items for that rollout

### Requirement: Child items

Each obligation SHALL yield a child item in its target repository, keyed
by the obligation key, according to its state:

- `pending` or `overdue`: `open-or-update`, titled with the action
  text, with a body and the target component's `owners` as assignees;
- `blocked`: `update-if-open`, with the same title and body;
- `done` or `waived`: `close-if-open`, with evidence
  `<state>: <evidence as code>`;
- `unknown`, `not-applicable`, or `error`: no item.

#### Scenario: Overdue child

- **WHEN** `allstar-source-updated` is overdue for `scorecard@v5.6.0`
- **THEN** the plan has `open-or-update ossf/allstar` titled
  `Bump github.com/ossf/scorecard/v5 to v5.6.0`

#### Scenario: Unknown obligation

- **WHEN** `infra-deployed` is `unknown` in a replay
- **THEN** the plan has no item for it

### Requirement: Child parent links

Rollout children SHALL record the parent's key and repository. Invariant
children SHALL have neither.

#### Scenario: Invariant child

- **WHEN** an invariant is overdue
- **THEN** its item has no `parent` or `parent_repo` in `plan --json`

### Requirement: Hidden marker

Every parent body SHALL start with
`<!-- baton key=<key> rules=<digest> -->`. Every child body SHALL start
with `<!-- baton key=<key> parent=<parent key> rules=<digest> -->`, with
an empty `parent=` for invariants. The writer SHALL find an issue again
by the first `<!-- baton key=<non-space>` in its body.

#### Scenario: Marker in JSON

- **WHEN** `plan --json` runs on the Scorecard example
- **THEN** the parent body begins with the marker, including a 12-digit
  `rules=` digest

### Requirement: Parent checklist

The parent body SHALL say `Follow-ups owed for <binding as code>.` and
list each obligation as `- [x]` or `- [ ]`, then the state in bold, the
target repository as code, the action text, and the evidence as code.
`done`, `waived`, and `not-applicable` SHALL be checked; every other
state SHALL be unchecked.

#### Scenario: Mixed checklist

- **WHEN** a rollout has done and overdue obligations
- **THEN** the done lines start `- [x] **done**` and the overdue line
  starts `- [ ] **overdue**`

### Requirement: Child body

A child body SHALL contain the marker, `State: **<state>**`,
`Evidence: <evidence as code>`, and the sentence
`This issue closes itself when the condition holds. Closing it by hand doesn't settle the obligation.`

#### Scenario: Blocked child body

- **WHEN** a child is planned for a blocked obligation
- **THEN** its body reads `State: **blocked**`

### Requirement: Untrusted text as code

Evidence and bindings, which come from repositories, tags, and
endpoints, SHALL be rendered as inline code. Runs of whitespace SHALL
collapse to one space. The fence SHALL be longer than the longest run of
backticks inside, and SHALL be padded with spaces when the text starts
or ends with a backtick. Action text from the baton file SHALL be
rendered as Markdown.

#### Scenario: Mention in evidence

- **WHEN** evidence contains `@someone` or `#123`
- **THEN** it appears inside a code span and doesn't mention or link

#### Scenario: Action is Markdown

- **WHEN** an action contains Markdown
- **THEN** the parent checklist renders it as Markdown, and the child
  title uses it unchanged
