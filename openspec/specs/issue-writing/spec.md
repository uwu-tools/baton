# issue-writing Specification

## Purpose

How `apply` carries out an issue plan on GitHub in the preview. This
covers the sandbox redirect, the `baton` label, and finding issues by
marker. It also covers opening, updating, reopening, closing, linking
sub-issues, and closing issues baton no longer tracks.

## Requirements

### Requirement: Sandbox redirect

The writer SHALL refuse to run without a redirect owner. With one, it
SHALL send every write for `<owner>/<name>` to `<redirect>/<name>`,
including the home repository. It SHALL prefix each title it writes with
`[<intended repo>]` and a space and SHALL drop assignees. Repositories
with the same name under different owners SHALL share one sandbox
repository.

#### Scenario: Redirected child

- **WHEN** a child is planned for `ex/action` with redirect `sandbox`
- **THEN** the issue is created in `sandbox/action` titled
  `[ex/action] <action>`, with no assignees

#### Scenario: Same repository name

- **WHEN** components `org-a/tool` and `org-b/tool` both have open
  obligations
- **THEN** both issues are created in `sandbox/tool`, told apart by
  their title prefixes

### Requirement: Label

Before its first use of a repository in a run, the writer SHALL check
for the `baton` label. If it's missing, the writer SHALL fail, unless
`--create-label` is set. In that case it SHALL create the label with
color `ededed` and description `Follow-up work tracked by baton`.

#### Scenario: Missing label

- **WHEN** a sandbox repository has no `baton` label and
  `--create-label` isn't set
- **THEN** apply fails with
  `<repo> has no "baton" label; create it first`

### Requirement: Finding issues

The writer SHALL list a repository's open and closed issues that carry
the `baton` label, excluding pull requests, and index them by the key in
their marker. When a key appears twice, it SHALL keep an open issue over
a closed one. Issues without the label SHALL be invisible to it.

#### Scenario: Duplicate key

- **WHEN** an open and a closed issue share a key
- **THEN** the writer works on the open one

### Requirement: Open or update

For `open-or-update`, the writer SHALL create a missing issue with the
`baton` label. For an existing issue, it SHALL reopen it if closed and
set the title and body when they differ, in one update. A reopen SHALL
add the comment `Reopened: this obligation is still unmet.` An issue
that already matches SHALL NOT be written.

#### Scenario: Hand-closed and retitled

- **WHEN** a maintainer closes an unmet obligation's issue and edits
  its title
- **THEN** the next apply reopens it, restores baton's title, and
  comments `Reopened: this obligation is still unmet.`

#### Scenario: New rules digest

- **WHEN** any byte of the baton file changes
- **THEN** every issue planned as `open-or-update`, and every open
  issue planned as `update-if-open`, gets a body with the new `rules=`
- **AND** open issues for `unknown`, `not-applicable`, or `error`
  obligations keep their old body

### Requirement: Update if open

For `update-if-open`, the writer SHALL update an open issue as
`open-or-update` would. It SHALL leave a closed or missing issue alone.

#### Scenario: Re-blocked step

- **WHEN** an obligation with an open issue becomes blocked
- **THEN** its issue stays open with body `State: **blocked**`

### Requirement: Close

For `close-if-open`, the writer SHALL comment `Done: <evidence>` on an
open issue and close it with reason `completed`. Waived obligations
SHALL close the same way, so the comment reads `Done: waived: ...`.
Closed or missing issues SHALL be left alone.

#### Scenario: Waiver added

- **WHEN** a waiver is added for an obligation with an open issue
- **THEN** the issue is closed as `completed` with a comment starting
  ``Done: waived: `waived by <approver> until <date>: <reason>``

### Requirement: Sub-issue links

After the parents, the writer SHALL link each `open-or-update` child
under its parent as a sub-issue, unless it's already linked. This holds
whether or not the child needed a write. `update-if-open` children SHALL
NOT be linked.

#### Scenario: Child linked once

- **WHEN** apply runs twice on the same pending child
- **THEN** the child is linked under the parent once

### Requirement: Superseding

After carrying out the plan, the writer SHALL check each repository it
loaded in this run. It SHALL close every open baton issue whose key
isn't in the report, as `not_planned`, with the comment
`Superseded: baton no longer tracks this obligation (a newer trigger replaced it, or the rule was removed).`
Repositories the plan didn't touch SHALL NOT be checked.

#### Scenario: Newer release

- **WHEN** a newer trigger release replaces the bound one
- **THEN** the old rollout's open issues are closed as superseded

### Requirement: Unbound rollouts supersede

Because an unbound rollout has no keys, a failed or unknowable trigger
lookup SHALL supersede that rollout's open issues in any repository
loaded for another item.

#### Scenario: Transient trigger failure

- **WHEN** a parent and a child are open, an invariant keeps their
  repositories in the plan, and the next run can't list the trigger's
  releases
- **THEN** both issues are closed as `not_planned` with the superseded
  comment
- **AND** the next successful run reopens them with the reopen comment

### Requirement: Order and failure

The writer SHALL carry out all parent items, then all child items, then
superseding. It SHALL stop at the first API error and report what it had
done. `apply` SHALL print each write as `apply: <action>`, indented two
spaces, and exit 2 on a write error.

#### Scenario: Write log

- **WHEN** apply opens a child
- **THEN** it prints
  `apply: opened sandbox/<name>#<n> [<repo>] <title>`
