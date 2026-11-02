# baton

[![ci](https://github.com/uwu-tools/baton/actions/workflows/ci.yml/badge.svg?branch=main)](https://github.com/uwu-tools/baton/actions/workflows/ci.yml)
[![OpenSSF Scorecard](https://api.scorecard.dev/projects/github.com/uwu-tools/baton/badge)](https://scorecard.dev/viewer/?uri=github.com/uwu-tools/baton)
[![Contributor-Covenant](https://img.shields.io/badge/Contributor%20Covenant-2.1-fbab2c.svg)](CODE_OF_CONDUCT.md)

baton tracks the follow-up work that a change in one repository owes
the others in a multi-repository project.

A release of a library owes a bump in each consumer, then a release of
each consumer, then a deploy. A copied schema owes a sync when the
source changes. These rules usually live in maintainers' heads, and
the work gets missed or arrives late. baton records them in one
`baton.yml` and checks them on a schedule. Each owed action stays open
as an issue in the owing repository until baton can verify, from the
repositories or the running service, that it's done.

> **Preview.** baton is pre-1.0. `apply` writes issues only to a
> sandbox owner given with `--redirect-owner`, never to the component
> repositories. Use `plan` to see what it would do there.

## How it works

- **`baton.yml`** lives in a dedicated `<project>-baton` repository.
  It lists the components, the rollouts each release triggers (a
  trigger plus obligations ordered by `needs`), standing invariants,
  and waivers.
- **Probes** answer "is this done?" as of a date, from `go.mod` files,
  releases, file contents, or a live endpoint, so the same file
  replays against history.
- **Issues.** Each rollout gets a parent issue in the baton repository
  with a checklist. Each pending or overdue obligation gets a child
  issue in the repository that owes it.

The full design, including states, probes, and the threat model, is
in [docs/design.md](docs/design.md). A complete example for OpenSSF
Scorecard is in
[examples/scorecard/baton.yml](examples/scorecard/baton.yml).

## Usage

```sh
go install github.com/uwu-tools/baton/cmd/baton@latest

baton validate baton.yml                        # offline checks, for PRs
baton plan baton.yml                            # states and issue changes; read-only
baton plan --as-of 2026-07-01 baton.yml         # the same, as of a past date
baton apply --redirect-owner my-sandbox baton.yml
baton version
```

`plan` and `apply` read `GITHUB_TOKEN`. Exit status is 0 on success,
1 for bad usage or config, and 2 when an obligation, API call, or
inventory check failed.

## Adopting baton

1. Create a `<project>-baton` repository and add a `baton.yml`.
2. Copy [templates/workflows/](templates/workflows/) into its
   `.github/workflows/`. The comments in
   [baton.yml](templates/workflows/baton.yml) cover the GitHub App and
   environment it needs.
3. Run `baton plan` and review the result before enabling the
   schedule.

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md). Please report security issues
as described in [SECURITY.md](SECURITY.md).

## License

Distributed under the Apache License 2.0. See [LICENSE](LICENSE).
