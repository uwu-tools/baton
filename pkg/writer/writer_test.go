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

package writer_test

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	bt "github.com/uwu-tools/baton/internal/batontest"
	"github.com/uwu-tools/baton/pkg/config"
	"github.com/uwu-tools/baton/pkg/plan"
	"github.com/uwu-tools/baton/pkg/reconcile"
	"github.com/uwu-tools/baton/pkg/writer"
	"github.com/uwu-tools/baton/pkg/writer/writerfakes"
)

const sandbox = "sandbox"

// tracker is an in-memory issue store behind a generated FakeIssues.
type tracker struct {
	t       *testing.T
	labels  map[string]bool
	issues  map[string][]*writer.Issue
	subs    map[string]map[int][]int64
	assign  map[int64][]string
	comment map[int64][]string
	reason  map[int64]string
	nextID  int64
	writes  int
}

func newTracker(t *testing.T) (*tracker, *writerfakes.FakeIssues) {
	t.Helper()
	tr := &tracker{
		t: t, labels: map[string]bool{}, issues: map[string][]*writer.Issue{},
		subs: map[string]map[int][]int64{}, assign: map[int64][]string{},
		comment: map[int64][]string{}, reason: map[int64]string{}, nextID: 1000,
	}
	f := &writerfakes.FakeIssues{}
	f.LabelExistsStub = func(repo, _ string) (bool, error) {
		tr.inSandbox(repo)

		return tr.labels[repo], nil
	}
	f.CreateLabelStub = func(repo, _, _, _ string) error {
		tr.inSandbox(repo)
		tr.writes++
		tr.labels[repo] = true

		return nil
	}
	f.ListIssuesStub = func(repo, _ string) ([]writer.Issue, error) {
		tr.inSandbox(repo)

		out := make([]writer.Issue, 0, len(tr.issues[repo]))
		for _, is := range tr.issues[repo] {
			out = append(out, *is)
		}

		return out, nil
	}
	f.CreateIssueStub = func(repo, title, body string, _, assignees []string) (writer.Issue, error) {
		tr.inSandbox(repo)
		tr.writes++
		tr.nextID++
		is := &writer.Issue{ID: tr.nextID, Number: len(tr.issues[repo]) + 1, State: "open", Title: title, Body: body}
		tr.issues[repo] = append(tr.issues[repo], is)
		tr.assign[is.ID] = assignees

		return *is, nil
	}
	f.UpdateIssueStub = func(repo string, n int, u writer.Update) (writer.Issue, error) {
		tr.inSandbox(repo)
		tr.writes++

		is := tr.find(repo, n)
		if u.State != "" {
			is.State = u.State
		}

		if u.StateReason != "" {
			tr.reason[is.ID] = u.StateReason
		}

		if u.Title != "" {
			is.Title = u.Title
		}

		if u.Body != "" {
			is.Body = u.Body
		}

		return *is, nil
	}
	f.CommentStub = func(repo string, n int, body string) error {
		tr.inSandbox(repo)
		tr.writes++
		is := tr.find(repo, n)
		tr.comment[is.ID] = append(tr.comment[is.ID], body)

		return nil
	}
	f.SubIssueIDsStub = func(repo string, n int) ([]int64, error) {
		tr.inSandbox(repo)

		return tr.subs[repo][n], nil
	}
	f.AddSubIssueStub = func(repo string, n int, id int64) error {
		tr.inSandbox(repo)

		tr.writes++
		if tr.subs[repo] == nil {
			tr.subs[repo] = map[int][]int64{}
		}

		tr.subs[repo][n] = append(tr.subs[repo][n], id)

		return nil
	}

	return tr, f
}

func (tr *tracker) inSandbox(repo string) {
	require.True(tr.t, strings.HasPrefix(repo, sandbox+"/"), "call outside sandbox: %s", repo)
}

func (tr *tracker) find(repo string, n int) *writer.Issue {
	for _, is := range tr.issues[repo] {
		if is.Number == n {
			return is
		}
	}

	tr.t.Fatalf("no issue %s#%d", repo, n)

	return nil
}

func (tr *tracker) byTitle(repo, prefix string) *writer.Issue {
	for _, is := range tr.issues[repo] {
		if strings.HasPrefix(is.Title, prefix) {
			return is
		}
	}

	return nil
}

const writerConfig = `
home: {repo: ex/baton}
components:
  core:   {repo: ex/core}
  action: {repo: ex/action, owners: [real-maintainer]}
rollouts:
  - id: core-release
    when: {release: core}
    obligations:
      - id: action-bumped
        target: action
        action: Bump core to {version}
        verify: {gomod: {module: github.com/example/core}}
        grace: 7d
      - id: action-released
        target: action
        needs: [action-bumped]
        action: Release action with core {version}
        verify: {release_gomod: {module: github.com/example/core}}
        grace: 7d
`

func apply(t *testing.T, api writer.Issues, cfg *config.Config, w *bt.World, asOf time.Time, create bool) error {
	t.Helper()

	rep, err := reconcile.New(&reconcile.Options{Config: cfg, Source: w.Source(), AsOf: asOf, Now: bt.Now}).Run()
	require.NoError(t, err)

	if _, err := writer.New(&writer.Options{RedirectOwner: sandbox, CreateLabel: create}, api).
		Run(&rep, plan.Plan(&rep)); err != nil {
		return fmt.Errorf("writer run: %w", err)
	}

	return nil
}

func TestLifecycle(t *testing.T) {
	t.Parallel()
	tr, api := newTracker(t)
	cfg := bt.Config(t, writerConfig)

	// Overdue release: parent in the home repo, child in the target
	// repo, both redirected and linked, with no assignees.
	require.NoError(t, apply(t, api, cfg, bt.History(), bt.EndOf("2026-02-15"), true))

	parent := tr.byTitle("sandbox/baton", "[ex/baton] [rollout] core v1.1.0")
	child := tr.byTitle("sandbox/action", "[ex/action] Release action with core v1.1.0")

	require.NotNil(t, parent)
	require.NotNil(t, child)
	require.Empty(t, tr.assign[child.ID], "redirect drops assignees")
	require.Equal(t, []int64{child.ID}, tr.subs["sandbox/baton"][parent.Number])
	require.True(t, tr.labels["sandbox/baton"])
	require.True(t, tr.labels["sandbox/action"])

	// Same state again: no writes.
	before := tr.writes

	require.NoError(t, apply(t, api, cfg, bt.History(), bt.EndOf("2026-02-15"), true))
	require.Equal(t, before, tr.writes, "rerun must not write")

	// Closed by hand while unmet: reopened with a comment.
	child.State = "closed"

	require.NoError(t, apply(t, api, cfg, bt.History(), bt.EndOf("2026-02-15"), true))
	require.Equal(t, "open", child.State)
	require.NotEmpty(t, tr.comment[child.ID])

	// Released: child and parent close as completed.
	require.NoError(t, apply(t, api, cfg, bt.History(), bt.EndOf("2026-02-21"), true))
	require.Equal(t, "closed", child.State)
	require.Equal(t, "completed", tr.reason[child.ID])
	require.Equal(t, "closed", parent.State)
	require.Equal(t, "completed", tr.reason[parent.ID])
}

func TestSupersedes(t *testing.T) {
	t.Parallel()
	tr, api := newTracker(t)
	cfg := bt.Config(t, writerConfig)
	w := bt.History()
	require.NoError(t, apply(t, api, cfg, w, bt.EndOf("2026-02-15"), true))

	// core v1.2.0 ships before action releases v1.1.0.
	w.Releases["ex/core"] = append(w.Releases["ex/core"], bt.History().Releases["ex/core"][1])
	w.Releases["ex/core"][2].Tag, w.Releases["ex/core"][2].Published = "v1.2.0", bt.Day("2026-02-16")
	require.NoError(t, apply(t, api, cfg, w, bt.EndOf("2026-02-17"), true))

	old := tr.byTitle("sandbox/baton", "[ex/baton] [rollout] core v1.1.0")
	require.Equal(t, "closed", old.State)
	require.Equal(t, "not_planned", tr.reason[old.ID])
	require.NotNil(t, tr.byTitle("sandbox/baton", "[ex/baton] [rollout] core v1.2.0"))
}

func TestLeavesErrorsAlone(t *testing.T) {
	t.Parallel()
	tr, api := newTracker(t)
	cfg := bt.Config(t, writerConfig)
	require.NoError(t, apply(t, api, cfg, bt.History(), bt.EndOf("2026-02-15"), true))

	broken := bt.History()
	broken.FailRepo = "ex/action"
	require.NoError(t, apply(t, api, cfg, broken, bt.EndOf("2026-02-15"), true))
	require.Equal(t, "open", tr.byTitle("sandbox/action", "[ex/action] Release action").State)
}

func TestKeepsReblockedIssueOpen(t *testing.T) {
	t.Parallel()
	tr, api := newTracker(t)
	cfg := bt.Config(t, writerConfig)
	require.NoError(t, apply(t, api, cfg, bt.History(), bt.EndOf("2026-02-15"), true))

	child := tr.byTitle("sandbox/action", "[ex/action] Release action")

	// The bump is reverted, so the release step is blocked again.
	w := bt.History()
	delete(w.Snapshots["ex/action"], "2026-02-03")
	require.NoError(t, apply(t, api, cfg, w, bt.EndOf("2026-02-15"), true))
	require.Equal(t, "open", child.State)
	require.Contains(t, child.Body, "**blocked**")
	require.Len(t, tr.issues["sandbox/action"], 2, "bump reopened as new owed work; release kept")
}

func TestMissingLabel(t *testing.T) {
	t.Parallel()
	tr, api := newTracker(t)
	err := apply(t, api, bt.Config(t, writerConfig), bt.History(), bt.Day("2026-02-15"), false)
	require.ErrorIs(t, err, writer.ErrMissingLabel)
	require.Zero(t, tr.writes)
}

func TestRequiresRedirect(t *testing.T) {
	t.Parallel()
	_, api := newTracker(t)
	_, err := writer.New(&writer.Options{}, api).Run(&reconcile.Report{}, nil)
	require.ErrorIs(t, err, writer.ErrNoRedirect)
	require.Zero(t, api.CreateIssueCallCount())
}
