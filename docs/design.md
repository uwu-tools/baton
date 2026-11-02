# baton: tracking the work one component owes another

When something happens in one component of a multi-repository project,
other components often owe follow-up work. baton records those rules
in one file and checks them on a schedule and on events. Each owed
action stays open as an issue until baton can verify, from the repos
or the running service, that it's done.

For Scorecard, an engine release owes three chains. The Action needs
a bump, then a release. Infra needs a bump, a release, and a
deployment. Allstar needs a bump, then a release. baton checks all of
these today and replays them against history.

> **Preview.** baton is pre-1.0. `apply` writes issues only to a
> sandbox owner (see [Running](#running)). The Scorecard example
> illustrates the format: its grace periods are illustrative, not
> Scorecard project policy, and it isn't affiliated with or endorsed
> by OpenSSF.

## Why

- **The rules exist only in maintainers' heads.** Scorecard's
  `RELEASE.md` covers the engine release; it doesn't list the
  follow-up bumps and releases in the Action, infra, or Allstar.
  `security-insights.yml` lists the seven repos but not how they
  depend on each other.
- **Owed work is easy to lose track of.** Scorecard v5.4.0
  (2025-11-14) and v5.5.0 (2026-04-23) reached an Action release in
  v2.4.4 on 2026-07-23, 251 and 91 days later; a release request
  ([ossf/scorecard-action#1679](https://github.com/ossf/scorecard-action/issues/1679))
  was open in the meantime. As of 2026-11-01, Allstar hadn't picked
  up v5.6.0; its Dependabot bump
  ([ossf/allstar#905](https://github.com/ossf/allstar/pull/905),
  opened 2026-10-19) was failing CI.
- **Existing tools stop at "a new version exists."** Dependabot and
  Renovate open PRs one repo at a time. Nothing chains a merged bump
  to a release and then to a deploy, or checks that the running
  service has the new version.

## Association types

The kinds of owed work baton is built to track. An obligation's
optional `type` names one of them, for reporting.

| Type                    | When A does this                                  | B owes                                           |
| ----------------------- | ------------------------------------------------- | ------------------------------------------------ |
| `version-consumption`   | Publishes a release                               | A bump of its pin                                |
| `release-chain`         | Is bumped in B                                    | A release of B, so B's users get A               |
| `merged-not-deployed`   | Merges a fix in a deployed component              | A release and a deploy                           |
| `data-contract`         | Changes a schema or result format                 | Updated copies, generated schemas, and consumers |
| `policy-coupling`       | Changes what it accepts, or B changes its example | A matching change on the other side              |
| `transitive-dependency` | Changes an indirect dependency                    | Code changes before it can take A                |
| `toolchain-floor`       | Raises its Go version or build image              | A raised floor, CI images, and builders          |
| `docs-mirror`           | Changes content B copies                          | An updated copy                                  |
| `endpoint-lifecycle`    | Retires a domain, registry, or image              | A move off it                                    |
| `ownership-move`        | Moves code, config, or issues to another repo     | Repointed docs, templates, and routing           |
| `security-advisory`     | Fixes a vulnerability                             | A bump and a release, and a notice to operators  |

## Shape

| Piece                                                     | Where                              |
| --------------------------------------------------------- | ---------------------------------- |
| `baton.yml`: components, rollouts, invariants, waivers    | `<project>-baton` repo             |
| Reconciler (the `baton` tool) on a schedule and on events | `<project>-baton` workflows        |
| GitHub App: read all component repos, write issues        | Installed on the org               |
| Parent rollout issues                                     | `<project>-baton` repo             |
| Child issues, one per owed action                         | The repo that owes it              |
| Release, deploy, and credentials                          | Each component's own repo          |
| Repository list                                           | `security-insights.yml`, unchanged |

**Why a dedicated repo.** Rollouts can start in any component: an
infra deploy owes checks for API consumers, and Allstar's results
format owes a monitor release. Keeping them out of `ossf/scorecard`'s
tracker leaves that tracker to the engine.
CODEOWNERS in the baton repo let each component's maintainers review
what their component owes. Kubernetes does the same:
`kubernetes/sig-release` holds release-team tracking issues, separate
from `kubernetes/kubernetes`.

**Why not a monorepo.** GitHub Marketplace requires each Action's
`action.yml` at a repo root, Allstar imports
`github.com/ossf/scorecard/v5`, and infra holds deploy credentials.
Merging the repos would break consumers and widen access, and
external downstreams would remain separate anyway.

## The file

```yaml
home: { repo: ossf/scorecard-baton }
inventory: { component: scorecard, path: security-insights.yml }

components:
  scorecard: { repo: ossf/scorecard }
  scorecard-action: { repo: ossf/scorecard-action, owners: [...] }
  scorecard-infra:
    { repo: ossf/scorecard-infra, source: tags, since: 2026-08-05 }
  allstar: { repo: ossf/allstar }

rollouts:
  - id: scorecard-release
    when: { release: scorecard }
    obligations:
      - id: action-source-updated
        target: scorecard-action
        action: Bump github.com/ossf/scorecard/v5 to {version}
        verify: { gomod: { module: github.com/ossf/scorecard/v5 } }
        grace: 7d
      - id: action-released
        target: scorecard-action
        needs: [action-source-updated]
        action: Release scorecard-action with scorecard {version}
        verify: { release_gomod: { module: github.com/ossf/scorecard/v5 } }
        grace: 14d
      # infra-source-updated, infra-released, infra-deployed,
      # allstar-source-updated, allstar-released ...

invariants:
  - id: infra-schema-copy
    target: scorecard-infra
    action: Sync cron/internal/format/json.v2.schema with ossf/scorecard
    verify:
      files_equal:
        source: { component: scorecard, path: pkg/scorecard/json.v2.schema }
        copy:
          {
            component: scorecard-infra,
            path: cron/internal/format/json.v2.schema,
          }

waivers:
  - obligation: scorecard-release/allstar-released
    binding: scorecard@v5.6.0
    approver: <maintainer>
    reason: <why>
    expires: 2026-12-01
```

The full Scorecard file is
[examples/scorecard/baton.yml](../examples/scorecard/baton.yml).

- **Inventory.** Every component must be listed in the project's
  Security Insights file. baton reads that list and doesn't extend
  the format.
- **Releases.** Each component says what counts as one of its
  releases, so projects that don't use SemVer, or tag in unusual ways,
  still work. The same definition serves triggers and probes such as
  `release_gomod`.

  ```yaml
  scorecard:
    repo: ossf/scorecard
    source: releases # forge releases (default) or tags
    releases:
      tag: '^v[0-9]+\.[0-9]+\.[0-9]+$' # only tags matching this
      prerelease: false # count prereleases (forge flag or -suffix)
      order: version # version (default), published, or forge
  ```

  `version` orders by SemVer and skips tags that aren't versions.
  `published` orders by publish date, for any tag scheme. `forge`
  uses the release the maintainers marked as latest. The forge flag
  can't be read for a past date, so replay reports unknown for those
  components. Drafts never count.

- **Triggers.** `when` names the component whose releases owe work,
  and can narrow which ones do. `level: minor` skips patch releases,
  and `level: major` skips minor ones too; it needs `order: version`.
  `tag` adds a pattern of its own. A narrowed trigger doesn't change
  what counts as a release for probes. A patch release of the Action
  still satisfies `release_gomod`.
- **Rollouts.** A trigger, plus the obligations it creates. Each
  rollout instance has a key made of the source repo, the tag, and the
  commit the tag points to, such as
  `ossf/scorecard:release:v5.6.0:61e84743…`, so reruns and duplicate
  events update the same issues. The commit makes the key unique even
  where tags can move. A release recut at the same commit keeps its
  rollout. A tag moved to another commit starts a new rollout, which
  supersedes the old one. `needs` orders obligations within a rollout,
  and the validator rejects cycles. Only the latest release is owed: a
  newer one supersedes the older rollout.
- **Rules digest.** Every issue marker records `rules=` and the first
  12 hex digits of the SHA-256 of `baton.yml`, so each issue shows
  which rules produced it. The digest changes only when the file does,
  so commits elsewhere in the baton repo don't touch issues.
- **Checks.** `verify` names one probe, or a list of probes that must
  all pass. One step can then require several facts, such as the bump
  at the release tag, the published image, and a passing smoke test.
  Each list entry names exactly one probe. An error in any check makes the
  step an error. A check that fails makes the step pending or overdue,
  even if another check is unknown. The step is unknown only when
  every other check passes. Evidence lists each check's result.
- **Invariants.** Checks with no trigger, such as copied schemas, docs
  that mirror another repo, or references to a retired domain.
- **Waivers.** These excuse an unmet obligation until they expire.
  Each needs an approver and a reason, and goes through PR review with
  the file. A waiver never hides an error. Steps that `need` a waived
  step inherit the waiver, because they can't be done without it.
- **Owners** are optional. Issues land in the owing repo either way.
- **Strict parsing.** A misspelled key is an error, not a silently
  ignored rule.

## States

| State          | Meaning                                                       | Issue                                                |
| -------------- | ------------------------------------------------------------- | ---------------------------------------------------- |
| done           | The check holds                                               | Child closed                                         |
| pending        | Not done, within `grace` of its clock start                   | Child open                                           |
| overdue        | Not done, past `grace` of its clock start                     | Child open                                           |
| blocked        | An obligation it `needs` isn't done; its clock hasn't started | No new child; an open one is kept and marked blocked |
| waived         | Not done, with an active waiver                               | Child closed, waiver noted                           |
| unknown        | Can't be checked for this date (live data in a replay)        | No change                                            |
| not-applicable | Target didn't exist yet (`since`)                             | None                                                 |
| error          | A probe or API call failed                                    | No change; exit status 2                             |

Each step has exactly one state. Signals that don't answer "is this
done?", such as a failing canary, are notes on the step. A note never
opens, closes, or blocks an issue.

**Clock start.** A step's `grace` runs from when it could first be
worked on. For a step with no `needs`, that's the trigger's publish
time. For a step with `needs`, it's the latest time a prerequisite
became done, as its probes report. A blocked step can't be overdue;
the overdue one is the prerequisite. When a prerequisite was waived or
not applicable, or its probes can't say when it became done, the
trigger time is used.

A rollout is complete when every obligation is done, waived, or not
applicable. Every obligation counts toward completion; there is no
non-blocking class. `ready`, `running`, and `failed` will be added
with dispatch (stage 2).

## Probes

Each probe answers one question as of a date, so the same file
replays against history.

| Probe            | Done when                                                                  | Done since                                                                | Types                                                               |
| ---------------- | -------------------------------------------------------------------------- | ------------------------------------------------------------------------- | ------------------------------------------------------------------- |
| `gomod`          | target's default branch requires the module at ≥ `{version}`               | the oldest commit in the unbroken run of `go.mod` commits that satisfy it | `version-consumption`, `transitive-dependency`, `security-advisory` |
| `release_gomod`  | target's latest release or tag requires it at ≥ `{version}`                | the oldest release in the unbroken run of releases that satisfy it        | `release-chain`, `security-advisory`                                |
| `deployed_gomod` | the commit a live endpoint reports requires it at ≥ `{version}`; live only | not known                                                                 | `merged-not-deployed`                                               |
| `files_equal`    | two files in two repos match                                               | not known                                                                 | `data-contract`, `docs-mirror`                                      |
| `file_lacks`     | a file doesn't contain a string                                            | not known                                                                 | `docs-mirror`, `endpoint-lifecycle`, `ownership-move`               |

A step with several checks became done when its last check did.
`gomod` reads at most 100 `go.mod` commits back. If all of them
satisfy it, the oldest one read is used, which can start the next
step's clock late but never early.

Version comparison uses Go module SemVer, so a pseudo-version for a
later commit counts as newer. Comparing what a release contains, not
its version number, handles independent version lines (scorecard
v5.6.0 shipped in Action v2.5.0).

Planned probes:

- `action_ref`: a workflow pins an action at ≥ `{version}`.
- `go_directive`: the target's Go floor is at least the source's.
- `workflow_conclusion`: a named workflow passed for a ref. This
  carries the released-Action smoke test and the production synthetic
  check, and turns the canary into evidence.

Canary results attach to obligations as evidence and never create
them. Raw canary status can be noisy (rate limits, transient
resolution failures), so a red canary alone isn't owed work.

## Running

- **Schedule.** Every six hours in the baton repo, in the template.
  The schedule is authoritative: it recomputes every obligation from
  what it observes, so missed events, manual fixes, and hand-closed
  issues all resolve.
- **Events.** Optional. A source repo's release workflow can send a
  `repository_dispatch` to the baton repo so it runs at once. Each
  sender then needs a credential for the baton repo, so the template
  leaves it off. An event decides when baton looks; what it observes
  decides whether the work is done.
- **Issues.** One parent per rollout instance
  (`[rollout] scorecard v5.6.0`), with a checklist that updates itself.
  Each pending or overdue obligation also gets a child issue in the
  owing repo, linked as a sub-issue. Every issue carries the `baton`
  label and a hidden `<!-- baton key=… -->` marker. A hand-closed
  issue whose obligation is still unmet is reopened.
- **Identity.** An org GitHub App, not a maintainer's token. It needs
  Contents read and Issues write, on every component and the baton
  repo. Each run mints a short-lived token with only those two
  permissions.
- **Templates.** [templates/workflows/](../templates/workflows/)
  holds the reconcile and validate workflows for a `<project>-baton`
  repo. They run a pinned baton release with `go run`, which checks the
  module against the Go checksum database.
- **Preview.** Until the write block is lifted, `apply` writes only
  with `--redirect-owner`, which sends every issue to a sandbox owner,
  titled with the repo it's meant for and without assignees. The
  templates run that way, with the App installed on the sandbox org.
  `plan` shows what baton would do in the real repos. Lifting the block
  is a separate release, made once the tool is trusted.

## Threat model

baton reads component repos and writes issues. Its one credential is
the GitHub App's private key.

| Threat                                                                  | Mitigation                                                                                                                                                            | Residual risk                                                                                            |
| ----------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------- |
| Someone edits `baton.yml` to drop an obligation or waive it             | Branch protection and CODEOWNERS on the baton repo; `validate` on every PR; waivers need an approver, a reason, and an expiry                                         | The approver field is text; PR review is the real control                                                |
| The App key leaks                                                       | The key is in a `baton` environment limited to the default branch; PR workflows get no secrets and use `pull_request`, not `pull_request_target`; tokens last an hour | A holder of the key can write issues and read repos where the App is installed, until the key is revoked |
| The App's scope grows                                                   | It asks for Contents read and Issues write only, so it can't push code, merge, or release                                                                             | A future need for more scope must be argued in review                                                    |
| Text from repos, tags, or endpoints mentions people or injects Markdown | Evidence and bindings render as inline code, so `@` mentions, `#` links, and Markdown don't take effect                                                               | Action text from `baton.yml`, with `{version}` filled in, renders as Markdown                            |
| A live endpoint reports a false deployed commit                         | The URL is declared in reviewed `baton.yml`; the commit must exist and its `go.mod` must satisfy the rule                                                             | baton trusts what the endpoint says is deployed                                                          |
| A tag moves or a release is recut                                       | Rollout keys include the tag's commit, so a moved tag starts a new rollout                                                                                            | None beyond the forge's own history                                                                      |
| A rule opens issues across many repos by mistake                        | `plan` previews every change; `--redirect-owner` sends writes to a test owner                                                                                         | A merged bad rule writes on the next run                                                                 |
| baton or its dependencies are compromised                               | Templates pin a release, checked against the Go checksum database; Actions pinned by commit; dependencies checked with Scorecard before adoption                      | A compromised release that is then pinned                                                                |
| Release events need a cross-repo credential                             | `repository_dispatch` is off by default; the schedule covers it                                                                                                       | Projects that turn it on hold one more credential per source repo                                        |

## The tool

`baton` is a Go CLI that follows SIG Release conventions
(kubernetes/release, release-utils, release-sdk):

| Command                                   | Does                                                     |
| ----------------------------------------- | -------------------------------------------------------- |
| `baton validate FILE`                     | Checks the file offline; for PRs                         |
| `baton plan FILE [--as-of DATE]`          | Evaluates and prints states and issue changes; read-only |
| `baton apply FILE --redirect-owner OWNER` | Makes the issue changes (sandbox only in the preview)    |
| `baton version`                           | Build info via release-utils                             |

Exit status is 0 on success, 1 for bad usage or config, and 2 when
an obligation, API call, or inventory check failed. The code lives in
`cmd/baton` (cobra) and `pkg/{config,source,probe,reconcile,plan,writer}`,
with the GitHub adapter (release-sdk plus go-github) in
`internal/github`. Build, lint, and test run through mage.

New dependencies must be actively maintained, checked with Scorecard's
`Maintained` check before they're added. Two build-time dependencies
score 0 there: `magefile/mage` (result of 2026-10-19) and
`uwu-tools/magex` (its latest published result, 2026-01-27). Neither ships in the baton binary. They stay
because mage is the SIG Release convention; magex is maintained by
uwu-tools itself.

## Stages

1. **Detect and surface.** baton evaluates, plans, and writes issues
   (`baton apply`), and the workflow templates run it on a schedule
   and validate `baton.yml` in PRs. It writes only with
   `--redirect-owner`, which sends every issue to a sandbox owner
   without assignees. Remaining: a project's baton repo adopting the
   templates, and lifting the write block.
2. **Remediate.** Each target repo exposes a `workflow_dispatch` that
   takes `baton_id`, `upstream_repository`, `upstream_version`, and
   `upstream_revision`. baton calls it to open the bump PR, cut the
   release, or request the deploy. The target workflow owns its
   credentials and approvals; production deploys stay behind the
   target's protected environment. Add `workflow_conclusion` probes
   and the `ready`, `running`, and `failed` states.
3. **Broaden.** Add contract triggers (API or schema version changes)
   for components such as a web app or monitor, and deploy-triggered
   rollouts for API consumers. Report lag metrics (release to bump, bump to
   release, release to deploy); the replay mode already computes them.

## Limits

- Probes cover what can be checked mechanically. A step like
  "announce the deprecation" needs a human-closed issue as its probe.
- Replay reads current repos at past commits. Deleted releases and
  force-pushed history can't be replayed, and live probes report
  `unknown`.
- Tags have no publish time, so baton dates them by commit time.
- Grace periods are policy. The file makes them explicit but doesn't
  choose them.
