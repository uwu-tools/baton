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

package plan_test

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	bt "github.com/uwu-tools/baton/internal/batontest"
	"github.com/uwu-tools/baton/pkg/plan"
	"github.com/uwu-tools/baton/pkg/reconcile"
)

func items(t *testing.T, asOf time.Time) map[string]plan.Item {
	t.Helper()
	cfg := bt.Config(t, strings.Replace(bt.BaseConfig, "action:     {repo: ex/action}",
		"action:     {repo: ex/action, owners: [ex/action-team]}", 1))
	rep, err := reconcile.New(&reconcile.Options{
		Config: cfg, Source: bt.History().Source(), AsOf: asOf, Now: bt.Now,
	}).Run()
	require.NoError(t, err)

	m := map[string]plan.Item{}

	items := plan.Plan(&rep)
	for i := range items {
		m[items[i].Key] = items[i]
	}

	return m
}

func TestPlanInProgress(t *testing.T) {
	t.Parallel()
	m := items(t, bt.EndOf("2026-02-15"))

	parent := m["ex/core:release:v1.1.0:sha-v1.1.0"]
	require.True(t, parent.IsParent)
	require.Equal(t, plan.OpenOrUpdate, parent.Op)
	require.Equal(t, "ex/core", parent.Repo)
	require.Contains(t, parent.Body, "[x] **done**")
	require.Contains(t, parent.Body, "[ ] **overdue**")
	require.Equal(t, "ex/core:release:v1.1.0:sha-v1.1.0", plan.KeyFromBody(parent.Body))

	rel := m["core-release/action-released@core@v1.1.0:sha-v1.1.0"]
	require.Equal(t, plan.OpenOrUpdate, rel.Op)
	require.Equal(t, parent.Key, rel.Parent)
	require.Equal(t, "ex/core", rel.ParentRepo)
	require.Equal(t, []string{"ex/action-team"}, rel.Assignees)
	require.Equal(t, "Release action with core v1.1.0", rel.Title)

	require.Equal(t, plan.CloseIfOpen, m["core-release/action-bumped@core@v1.1.0:sha-v1.1.0"].Op)
	require.NotContains(t, m, "core-release/late-bumped@core@v1.1.0:sha-v1.1.0", "not-applicable gets no issue")
}

func TestPlanBlockedOnlyUpdates(t *testing.T) {
	t.Parallel()
	m := items(t, bt.EndOf("2026-02-02"))
	require.Equal(t, plan.UpdateIfOpen, m["core-release/action-released@core@v1.1.0:sha-v1.1.0"].Op)
}

func TestPlanCompleteClosesParent(t *testing.T) {
	t.Parallel()
	m := items(t, bt.EndOf("2026-02-21"))
	require.Equal(t, plan.CloseIfOpen, m["ex/core:release:v1.1.0:sha-v1.1.0"].Op)
}

func TestMarkersCarryRulesDigest(t *testing.T) {
	t.Parallel()
	m := items(t, bt.EndOf("2026-02-15"))

	parent := m["ex/core:release:v1.1.0:sha-v1.1.0"]
	require.Regexp(t, `<!-- baton key=ex/core:release:v1.1.0:sha-v1.1.0 rules=[0-9a-f]{12} -->`, parent.Body)

	child := m["core-release/action-released@core@v1.1.0:sha-v1.1.0"]
	require.Regexp(t, ` parent=ex/core:release:v1.1.0:sha-v1.1.0 rules=[0-9a-f]{12} -->`, child.Body)
	require.Equal(t, child.Key, plan.KeyFromBody(child.Body))
}

func TestCodeSpan(t *testing.T) {
	t.Parallel()

	for in, want := range map[string]string{
		"":                              "",
		"deployed abc requires v1.0":    "`deployed abc requires v1.0`",
		"ping @example/maintainers #12": "`ping @example/maintainers #12`",
		"two\nlines  here":              "`two lines here`",
		"has `code` inside":             "``has `code` inside``",
		"`edge":                         "`` `edge ``",
	} {
		require.Equal(t, want, plan.CodeSpan(in), in)
	}
}

func TestEvidenceCantMention(t *testing.T) {
	t.Parallel()

	// A live endpoint reports a "commit" that is really a mention.
	cfg := bt.Config(t, bt.BaseConfig+`
      - id: deployed
        target: action
        action: Deploy core {version}
        verify: {deployed_gomod: {url: "https://svc/version", module: github.com/example/core}}
`)
	w := bt.History()
	w.JSON["https://svc/version"] = map[string]any{"commit": "@everyone"}

	rep, err := reconcile.New(&reconcile.Options{Config: cfg, Source: w.Source(), AsOf: bt.Now, Now: bt.Now}).Run()
	require.NoError(t, err)
	require.Contains(t, rep.All()[3].Evidence, "@everyone", "the untrusted text reaches the evidence")

	for _, it := range plan.Plan(&rep) {
		for _, text := range []string{it.Body, it.Evidence} {
			for line := range strings.SplitSeq(text, "\n") {
				if !strings.HasPrefix(line, "<!-- baton") {
					require.NotContains(t, stripCode(line), "@", "an @ outside a code span: %q", line)
				}
			}
		}
	}
}

// stripCode removes inline code spans: a run of n backticks up to the
// next run of exactly n.
func stripCode(s string) string {
	var b strings.Builder

	for i := 0; i < len(s); {
		if s[i] != '`' {
			b.WriteByte(s[i])
			i++

			continue
		}

		n := 0
		for i+n < len(s) && s[i+n] == '`' {
			n++
		}

		end := -1

		for j := i + n; j < len(s); {
			if s[j] != '`' {
				j++

				continue
			}

			m := 0
			for j+m < len(s) && s[j+m] == '`' {
				m++
			}

			if m == n {
				end = j + m

				break
			}

			j += m
		}

		if end < 0 {
			b.WriteString(s[i:])

			break
		}

		i = end
	}

	return b.String()
}
