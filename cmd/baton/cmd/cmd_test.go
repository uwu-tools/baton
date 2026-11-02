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

package cmd_test

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/uwu-tools/baton/cmd/baton/cmd"
	bt "github.com/uwu-tools/baton/internal/batontest"
	"github.com/uwu-tools/baton/pkg/writer"
	"github.com/uwu-tools/baton/pkg/writer/writerfakes"
)

func execute(t *testing.T, w *bt.World, issues writer.Issues, args ...string) (string, error) {
	t.Helper()

	calls := 0
	factory := func() (*cmd.Clients, error) {
		calls++

		return &cmd.Clients{Source: w.Source(), Issues: issues}, nil
	}
	root := cmd.New(factory, func() time.Time { return bt.EndOf("2026-02-15") })

	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs(args)
	err := root.Execute()

	if len(args) > 0 && args[0] == "validate" {
		require.Zero(t, calls, "validate must stay offline")
	}

	return out.String(), err
}

func TestValidate(t *testing.T) {
	t.Parallel()
	out, err := execute(t, bt.History(), nil, "validate", bt.WriteFile(t, bt.BaseConfig))
	require.NoError(t, err)
	require.Contains(t, out, "1 rollout(s), 0 invariant(s), 0 waiver(s): ok")

	bad := strings.Replace(bt.BaseConfig, "target: late", "target: nope", 1)
	_, err = execute(t, bt.History(), nil, "validate", bt.WriteFile(t, bad))
	require.ErrorContains(t, err, "unknown target")
	require.NotErrorIs(t, err, cmd.ErrEvaluation, "config errors exit 1, not 2")
}

func TestPlan(t *testing.T) {
	t.Parallel()
	out, err := execute(t, bt.History(), nil, "plan", "--as-of", "2026-02-15", bt.WriteFile(t, bt.BaseConfig))
	require.NoError(t, err)
	require.Contains(t, out, "overdue")
	require.Contains(t, out, "plan: open-or-update")

	broken := bt.History()
	broken.FailRepo = "ex/action"
	_, err = execute(t, broken, nil, "plan", bt.WriteFile(t, bt.BaseConfig))
	require.ErrorIs(t, err, cmd.ErrEvaluation, "probe failures exit 2")

	_, err = execute(t, bt.History(), nil, "plan", "--as-of", "Feb", bt.WriteFile(t, bt.BaseConfig))
	require.ErrorContains(t, err, "--as-of")
}

func TestApplyRequiresRedirect(t *testing.T) {
	t.Parallel()

	api := &writerfakes.FakeIssues{}
	_, err := execute(t, bt.History(), api, "apply", bt.WriteFile(t, bt.BaseConfig))
	require.ErrorIs(t, err, writer.ErrNoRedirect)
	require.Zero(t, api.CreateIssueCallCount()+api.UpdateIssueCallCount()+api.CommentCallCount())
}
