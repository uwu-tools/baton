# How to Contribute

Thanks for your interest in contributing to `baton`! Here are a few general
guidelines on contributing and reporting bugs that we ask you to review.
Following these guidelines helps to communicate that you respect the time of the
contributors managing and developing this open source project. In return, they
should reciprocate that respect in addressing your issue, assessing changes, and
helping you finalize your pull requests.

Please note that all of your interactions in the project are subject to our
[Code of Conduct](/CODE_OF_CONDUCT.md). This includes creation of issues or pull
requests, commenting on issues or pull requests, and extends to all interactions
in any real-time space e.g., Slack, Discord, etc.

## Reporting Issues

Before reporting a new issue, please ensure that the issue was not already
reported or fixed by searching through our [issues
list](https://github.com/uwu-tools/baton/issues).

When creating a new issue, please be sure to include a **title and clear
description**, as much relevant information as possible, and, if possible, a
test case.

**If you discover a security bug, please do not report it through GitHub issues.
Instead, please see security procedures in [SECURITY.md](/SECURITY.md).**

## Sending Pull Requests

Before sending a new pull request, take a look at existing pull requests and
issues to see if the proposed change or fix has been discussed in the past, or
if the change was already implemented but not yet released.

We expect new pull requests to include tests for any affected behavior, and, as
we follow semantic versioning, we may reserve breaking changes until the next
major version release.

### Developer Certificate of Origin (DCO)

This project requires a [Developer Certificate of Origin][dco] (DCO) sign-off on
every commit. Add one automatically with the `-s` flag:

```sh
git commit -s -m "your message"
```

This appends a `Signed-off-by: Your Name <your@email>` trailer certifying that
you wrote or have the right to submit the change.

[dco]: https://developercertificate.org/

### Development

baton is a Go CLI that follows the conventions of the Kubernetes SIG Release
tools. Build, lint, and test run through [mage](https://magefile.org), with no
install needed:

```sh
go run mage.go all        # boilerplate, go.mod, golangci-lint, unit tests
go run mage.go test       # unit tests only
go run mage.go e2eTest    # live replay of Scorecard history; needs GITHUB_TOKEN
go run mage.go generate   # regenerate the counterfeiter fakes
```

- Commands live in `cmd/baton`; each area is a package under `pkg/` with an
  `Options` struct and `New(opts).Run()`.
- Unit tests use counterfeiter fakes and never touch the network. Tests that do
  go behind the `e2e` build tag in `test/e2e`.
- New dependencies must be actively maintained. Check them with the OpenSSF
  Scorecard `Maintained` check before adding them.
- Every Go file carries the Apache-2.0 header in
  `scripts/boilerplate/boilerplate.go.txt`.

## Other Ways to Contribute

We welcome anyone that wants to contribute to `baton` to triage and reply to
open issues to help troubleshoot and fix existing bugs. Here is what you can do:

- Help ensure that existing issues follow the recommendations from the
  _[Reporting Issues](#reporting-issues)_ section, providing feedback to the
  issue's author on what might be missing.
- Review existing pull requests, and test patches against real projects that
  use `baton`.
- Write a test, or add a missing test case to an existing test.

Thanks again for your interest in contributing to `baton`!

:heart:
