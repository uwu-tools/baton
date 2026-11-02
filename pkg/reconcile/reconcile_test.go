/*
Copyright 2026 The uwu-tools Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package reconcile_test

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	bt "github.com/uwu-tools/baton/internal/batontest"
	"github.com/uwu-tools/baton/pkg/config"
	"github.com/uwu-tools/baton/pkg/reconcile"
	"github.com/uwu-tools/baton/pkg/source"
)

func run(t *testing.T, cfg *config.Config, src source.Source, asOf time.Time) *reconcile.Report {
	t.Helper()

	rep, err := reconcile.New(&reconcile.Options{Config: cfg, Source: src, AsOf: asOf, Now: bt.Now}).Run()
	require.NoError(t, err)

	return &rep
}

func states(rep *reconcile.Report) map[string]reconcile.State {
	m := map[string]reconcile.State{}

	all := rep.All()
	for i := range all {
		m[all[i].ID] = all[i].State
	}

	return m
}

func TestRolloutOverTime(t *testing.T) {
	t.Parallel()

	cfg := bt.Config(t, bt.BaseConfig)
	for _, tc := range []struct {
		asOf     string
		want     map[string]reconcile.State
		complete bool
	}{
		// Known-good: v1.0.0 was already in action v1.9.0.
		{"2026-01-05", map[string]reconcile.State{
			"core-release/action-bumped":   reconcile.Done,
			"core-release/action-released": reconcile.Done,
			"core-release/late-bumped":     reconcile.NotApplicable,
		}, true},
		// Inside grace, before the bump: pending, and the release waits.
		{"2026-02-02", map[string]reconcile.State{
			"core-release/action-bumped":   reconcile.Pending,
			"core-release/action-released": reconcile.Blocked,
		}, false},
		// Known-bad: bumped on main but not released after grace.
		{"2026-02-15", map[string]reconcile.State{
			"core-release/action-bumped":   reconcile.Done,
			"core-release/action-released": reconcile.Overdue,
		}, false},
		{"2026-02-21", map[string]reconcile.State{
			"core-release/action-bumped":   reconcile.Done,
			"core-release/action-released": reconcile.Done,
		}, true},
	} {
		t.Run(tc.asOf, func(t *testing.T) {
			t.Parallel()
			rep := run(t, cfg, bt.History().Source(), bt.EndOf(tc.asOf))

			got := states(rep)
			for id, want := range tc.want {
				require.Equal(t, want, got[id], id)
			}

			require.Equal(t, tc.complete, rep.Instances[0].Complete)
		})
	}
}

func TestOverdueAge(t *testing.T) {
	t.Parallel()
	rep := run(t, bt.Config(t, bt.BaseConfig), bt.History().Source(), bt.EndOf("2026-02-15"))
	r := rep.Instances[0].Results[1]
	require.NotNil(t, r.AgeDays)
	require.Equal(t, 12, *r.AgeDays, "counted from the bump on 02-03, not the release on 02-01")
}

func TestClockStartsWhenPrerequisitesAreDone(t *testing.T) {
	t.Parallel()

	cfg := bt.Config(t, bt.BaseConfig)
	w := bt.History()
	// The bump lands late, 11 days after the core release.
	w.Snapshots["ex/action"] = map[string]string{"2026-01-01": bt.GoMod("v1.0.0"), "2026-02-12": bt.GoMod("v1.1.0")}

	rep := run(t, cfg, w.Source(), bt.EndOf("2026-02-14"))
	bumped, released := rep.Instances[0].Results[0], rep.Instances[0].Results[1]
	require.Equal(t, reconcile.Done, bumped.State)
	require.NotNil(t, bumped.DoneSince)
	require.Equal(t, bt.Day("2026-02-12"), *bumped.DoneSince)
	require.Equal(t, reconcile.Pending, released.State, "two days after the bump, not overdue from the release")

	rep = run(t, cfg, w.Source(), bt.EndOf("2026-02-20"))
	require.Equal(t, reconcile.Done, rep.Instances[0].Results[1].State)
	require.Equal(t, bt.Day("2026-02-20"), *rep.Instances[0].Results[1].DoneSince,
		"release_gomod reports the release date")

	// Blocked steps never run their own clock.
	rep = run(t, cfg, w.Source(), bt.EndOf("2026-02-11"))
	require.Equal(t, reconcile.Overdue, rep.Instances[0].Results[0].State, "the prerequisite is the overdue one")
	require.Equal(t, reconcile.Blocked, rep.Instances[0].Results[1].State)
	require.Nil(t, rep.Instances[0].Results[1].AgeDays)
}

func TestDoneSinceIsTheLatestUnbrokenRun(t *testing.T) {
	t.Parallel()

	cfg := bt.Config(t, bt.BaseConfig)
	w := bt.History()
	// Bumped, reverted, bumped again, then bumped further.
	w.Snapshots["ex/action"] = map[string]string{
		"2026-01-01": bt.GoMod("v1.0.0"),
		"2026-02-03": bt.GoMod("v1.1.0"),
		"2026-02-05": bt.GoMod("v1.0.0"),
		"2026-02-07": bt.GoMod("v1.1.0"),
		"2026-02-09": bt.GoMod("v1.2.0"),
	}

	rep := run(t, cfg, w.Source(), bt.EndOf("2026-02-10"))
	require.Equal(t, bt.Day("2026-02-07"), *rep.Instances[0].Results[0].DoneSince)
}

func TestWaivedPrerequisiteCountsFromTrigger(t *testing.T) {
	t.Parallel()

	start := reconcile.ClockStart(bt.Day("2026-02-01"), []string{"a", "b"}, map[string]*reconcile.Result{
		"a": {State: reconcile.Waived},
		"b": {State: reconcile.Done}, // done, but its probes can't say when
	})
	require.Equal(t, bt.Day("2026-02-01"), start)
}

func TestWaivers(t *testing.T) {
	t.Parallel()

	waived := bt.BaseConfig + `
waivers:
  - obligation: core-release/action-released
    binding: core@v1.1.0
    approver: someone
    reason: release scheduled with the next feature
    expires: 2026-02-16
`
	cfg := bt.Config(t, waived)
	id := "core-release/action-released"
	require.Equal(t, reconcile.Waived, states(run(t, cfg, bt.History().Source(), bt.Day("2026-02-15")))[id])
	require.Equal(t, reconcile.Overdue, states(run(t, cfg, bt.History().Source(), bt.Day("2026-02-17")))[id])

	other := bt.Config(t, strings.Replace(waived, "core@v1.1.0", "core@v9.9.9", 1))
	require.Equal(t, reconcile.Overdue, states(run(t, other, bt.History().Source(), bt.Day("2026-02-15")))[id],
		"a waiver for another binding doesn't apply")
}

func TestWaiverIsInherited(t *testing.T) {
	t.Parallel()
	cfg := bt.Config(t, bt.BaseConfig+`
waivers:
  - obligation: core-release/action-bumped
    approver: someone
    reason: action is frozen this cycle
    expires: 2026-03-01
`)
	w := bt.History()
	delete(w.Snapshots["ex/action"], "2026-02-03") // never bumped
	rep := run(t, cfg, w.Source(), bt.Day("2026-02-15"))
	got := states(rep)
	require.Equal(t, reconcile.Waived, got["core-release/action-bumped"])
	require.Equal(t, reconcile.Waived, got["core-release/action-released"])
	require.True(t, rep.Instances[0].Complete)
}

func TestErrorsStayErrors(t *testing.T) {
	t.Parallel()
	cfg := bt.Config(t, bt.BaseConfig+`
waivers:
  - obligation: core-release/action-bumped
    approver: someone
    reason: test
    expires: 2099-01-01
`)
	w := bt.History()
	w.FailRepo = "ex/action"
	got := states(run(t, cfg, w.Source(), bt.Day("2026-02-15")))
	require.Equal(t, reconcile.Error, got["core-release/action-bumped"], "a waiver never hides an error")
	require.Equal(t, reconcile.Blocked, got["core-release/action-released"])

	w = bt.History()
	w.FailRepo = "ex/core"
	rep := run(t, cfg, w.Source(), bt.Day("2026-02-15"))
	require.True(t, rep.HasErrors())

	all := rep.All()
	for i := range all {
		require.Equal(t, reconcile.Error, all[i].State, all[i].ID)
	}
}

func TestLiveProbeIsUnknownInThePast(t *testing.T) {
	t.Parallel()
	cfg := bt.Config(t, `
components:
  core: {repo: ex/core}
  action: {repo: ex/action}
rollouts:
  - id: r
    when: {release: core}
    obligations:
      - id: deployed
        target: action
        action: Deploy {version}
        verify: {deployed_gomod: {url: "https://svc/version", module: github.com/example/core}}
`)
	w := bt.History()
	w.JSON["https://svc/version"] = map[string]any{"commit": "v2.0.0"}
	require.Equal(t, reconcile.Unknown, states(run(t, cfg, w.Source(), bt.Day("2026-03-01")))["r/deployed"])
	require.Equal(t, reconcile.Done, states(run(t, cfg, w.Source(), bt.Now))["r/deployed"])
	w.JSON["https://svc/version"] = map[string]any{"version": "x"}
	require.Equal(t, reconcile.Error, states(run(t, cfg, w.Source(), bt.Now))["r/deployed"])
}

func TestInvariants(t *testing.T) {
	t.Parallel()
	cfg := bt.Config(t, `
components:
  core: {repo: ex/core}
  action: {repo: ex/action}
invariants:
  - id: schema
    target: action
    action: Sync schema
    verify:
      files_equal:
        source: {component: core, path: s.json}
        copy: {component: action, path: s.json}
  - id: old-domain
    target: action
    action: Drop old domain
    verify: {file_lacks: {path: README.md, text: old.example}}
`)
	w := bt.History()
	w.Files["ex/core/s.json@ex/core@2026-01-01"] = "{}"
	w.Files["ex/action/s.json@ex/action@2026-02-03"] = "{}"
	w.Files["ex/action/README.md@ex/action@2026-02-03"] = "a\nsee old.example\n"
	got := states(run(t, cfg, w.Source(), bt.Day("2026-03-01")))
	require.Equal(t, reconcile.Done, got["schema"])
	require.Equal(t, reconcile.Overdue, got["old-domain"])

	w.Files["ex/action/s.json@ex/action@2026-02-03"] = `{"x":1}`
	require.Equal(t, reconcile.Overdue, states(run(t, cfg, w.Source(), bt.Day("2026-03-01")))["schema"])
}

func TestInventory(t *testing.T) {
	t.Parallel()
	cfg := bt.Config(t, bt.BaseConfig+"\ninventory: {component: core, path: si.yml}\n")
	w := bt.History()
	w.Files["ex/core/si.yml@ex/core@2026-01-01"] = `
project:
  repositories:
    - url: https://github.com/ex/core
    - url: https://github.com/ex/action
`
	err := reconcile.CheckInventory(cfg, w.Source(), bt.Now)
	require.ErrorIs(t, err, reconcile.ErrInventory)
	require.ErrorContains(t, err, "late (ex/late)")

	w.Files["ex/core/si.yml@ex/core@2026-01-01"] += "    - url: https://github.com/ex/late\n"
	require.NoError(t, reconcile.CheckInventory(cfg, w.Source(), bt.Now))
}

func TestOptionsValidate(t *testing.T) {
	t.Parallel()

	_, err := reconcile.New(&reconcile.Options{}).Run()
	require.Error(t, err)
	_, err = reconcile.New(&reconcile.Options{
		Config: bt.Config(t, bt.BaseConfig), Source: bt.History().Source(),
		AsOf: bt.Now.Add(time.Hour), Now: bt.Now,
	}).Run()
	require.ErrorContains(t, err, "in the future")
}

func TestRolloutKeyIncludesTagCommit(t *testing.T) {
	t.Parallel()

	cfg := bt.Config(t, bt.BaseConfig)
	w := bt.History()
	rep := run(t, cfg, w.Source(), bt.EndOf("2026-02-15"))
	require.Equal(t, "ex/core:release:v1.1.0:sha-v1.1.0", rep.Instances[0].Key)
	require.Equal(t, "core-release/action-released@core@v1.1.0:sha-v1.1.0", rep.Instances[0].Results[1].Key())
	require.Equal(t, cfg.Digest, rep.Rules)

	// A tag moved to another commit is a new rollout with new keys.
	w.Tags = map[string]string{"ex/core@v1.1.0": "moved"}
	moved := run(t, cfg, w.Source(), bt.EndOf("2026-02-15"))
	require.Equal(t, "ex/core:release:v1.1.0:moved", moved.Instances[0].Key)
	require.Equal(t, "core-release/action-released@core@v1.1.0:moved", moved.Instances[0].Results[1].Key())

	// Invariants have no trigger commit.
	require.Equal(t, "x@always", (&reconcile.Result{ID: "x", Binding: "always"}).Key())
}

func TestListedCommitSkipsLookup(t *testing.T) {
	t.Parallel()

	w := bt.History()
	w.Releases["ex/core"][1].Commit = "listed"
	src := w.Source()
	rep := run(t, bt.Config(t, bt.BaseConfig), src, bt.EndOf("2026-02-15"))
	require.Equal(t, "ex/core:release:v1.1.0:listed", rep.Instances[0].Key)
	require.Zero(t, src.TagCommitCallCount())
}

func TestTagCommitErrorIsError(t *testing.T) {
	t.Parallel()

	w := bt.History()
	src := w.Source()
	src.TagCommitReturns("", bt.ErrBoom)
	src.TagCommitStub = nil
	rep := run(t, bt.Config(t, bt.BaseConfig), src, bt.EndOf("2026-02-15"))
	require.True(t, rep.HasErrors())
	require.Empty(t, rep.Instances[0].Key, "no key without a commit")

	for _, r := range rep.Instances[0].Results {
		require.Equal(t, reconcile.Error, r.State)
	}
}

func TestCheckListNeedsEveryCheck(t *testing.T) {
	t.Parallel()

	cfg := bt.Config(t, strings.Replace(bt.BaseConfig,
		"verify: {release_gomod: {module: github.com/example/core}}",
		"verify: [{release_gomod: {module: github.com/example/core}}, {gomod: {module: github.com/example/core}}]", 1))

	// Bumped on main, not released: one check fails, so overdue.
	rep := run(t, cfg, bt.History().Source(), bt.EndOf("2026-02-15"))
	res := rep.Instances[0].Results[1]
	require.Equal(t, reconcile.Overdue, res.State)
	require.Contains(t, res.Evidence, "release_gomod: ")
	require.Contains(t, res.Evidence, "; gomod: ")

	rep = run(t, cfg, bt.History().Source(), bt.EndOf("2026-02-21"))
	require.Equal(t, reconcile.Done, rep.Instances[0].Results[1].State)
}

func TestCheckListUnknownAndErrors(t *testing.T) {
	t.Parallel()

	cfg := bt.Config(t, `
components:
  core: {repo: ex/core}
  action: {repo: ex/action}
rollouts:
  - id: r
    when: {release: core}
    obligations:
      - id: shipped
        target: action
        action: Ship {version}
        grace: 7d
        verify:
          - release_gomod: {module: github.com/example/core}
          - deployed_gomod: {url: "https://svc/version", module: github.com/example/core}
`)
	w := bt.History()
	w.JSON["https://svc/version"] = map[string]any{"commit": "v2.0.0"}

	// The live check is unknowable in the past. Before the release
	// the other check fails, which decides it: overdue.
	require.Equal(t, reconcile.Overdue, states(run(t, cfg, w.Source(), bt.EndOf("2026-02-15")))["r/shipped"])
	// After the release the rest pass, so the step is unknown.
	require.Equal(t, reconcile.Unknown, states(run(t, cfg, w.Source(), bt.EndOf("2026-03-01")))["r/shipped"])
	require.Equal(t, reconcile.Done, states(run(t, cfg, w.Source(), bt.Now))["r/shipped"])

	// An error in any check wins.
	w.JSON["https://svc/version"] = map[string]any{"version": "x"}
	rep := run(t, cfg, w.Source(), bt.Now)
	require.Equal(t, reconcile.Error, states(rep)["r/shipped"])
	require.Contains(t, rep.Instances[0].Results[0].Evidence, "deployed_gomod: ")
}

func TestTriggerLevel(t *testing.T) {
	t.Parallel()

	w := bt.History()
	w.Releases["ex/core"] = append(w.Releases["ex/core"], source.Release{Tag: "v1.1.1", Published: bt.Day("2026-02-10")})

	every := run(t, bt.Config(t, bt.BaseConfig), w.Source(), bt.EndOf("2026-02-12"))
	require.Equal(t, "core@v1.1.1", every.Instances[0].Binding)

	minor := run(t, bt.Config(t, strings.Replace(bt.BaseConfig, "when: {release: core}",
		"when: {release: core, level: minor}", 1)), w.Source(), bt.EndOf("2026-02-12"))
	require.Equal(t, "core@v1.1.0", minor.Instances[0].Binding, "a patch release doesn't owe work")
}

func TestForgeOrderIsUnknownInThePast(t *testing.T) {
	t.Parallel()

	cfg := bt.Config(t, strings.Replace(bt.BaseConfig, "core:       {repo: ex/core}",
		"core:       {repo: ex/core, releases: {order: forge}}", 1))
	src := bt.History().Source()
	src.FlaggedLatestReturns(source.Release{Tag: "v1.1.0"}, nil)

	got := states(run(t, cfg, src, bt.EndOf("2026-02-15")))
	require.Equal(t, reconcile.Unknown, got["core-release/action-bumped"])
	require.False(t, run(t, cfg, src, bt.EndOf("2026-02-15")).HasErrors())

	live := run(t, cfg, src, bt.Now)
	require.Equal(t, "core@v1.1.0", live.Instances[0].Binding)
}
