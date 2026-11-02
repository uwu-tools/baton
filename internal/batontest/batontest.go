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

// Package batontest holds shared fixtures for baton's tests.
package batontest

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/uwu-tools/baton/pkg/config"
	"github.com/uwu-tools/baton/pkg/probe"
	"github.com/uwu-tools/baton/pkg/source"
	"github.com/uwu-tools/baton/pkg/source/sourcefakes"
)

// ErrBoom is what a failing fake returns.
var ErrBoom = errors.New("boom")

// Day parses YYYY-MM-DD as UTC midnight.
func Day(s string) time.Time {
	t, err := time.Parse(time.DateOnly, s)
	if err != nil {
		panic(err)
	}

	return t
}

// EndOf returns the last second of a YYYY-MM-DD day.
func EndOf(s string) time.Time { return Day(s).Add(24*time.Hour - time.Second) }

// Now is the fixed "now" for fixtures.
var Now = Day("2026-06-01")

// GoMod returns a go.mod that requires github.com/example/core at v.
func GoMod(v string) string {
	return "module example.com/x\n\ngo 1.26\n\nrequire github.com/example/core " + v + "\n"
}

// World is the data behind a fake Source. Commits are named
// "<repo>@<YYYY-MM-DD>" for the latest dated snapshot at or before asOf.
type World struct {
	Releases  map[string][]source.Release  // repo -> releases
	Snapshots map[string]map[string]string // repo -> date -> go.mod
	Files     map[string]string            // "repo/path@ref" -> content
	JSON      map[string]map[string]any    // url -> body
	Tags      map[string]string            // "repo@tag" -> commit; default "sha-<tag>"
	FailRepo  string                       // every call for this repo errors
}

// History is the standard scenario. core releases v1.0.0 on 01-01 and
// v1.1.0 on 02-01. action bumps to v1.1.0 on 02-03 and releases v2.0.0
// with it on 02-20. late appears 03-01, already on v1.1.0.
func History() *World {
	return &World{
		Releases: map[string][]source.Release{
			"ex/core":   {{Tag: "v1.0.0", Published: Day("2026-01-01")}, {Tag: "v1.1.0", Published: Day("2026-02-01")}},
			"ex/action": {{Tag: "v2.0.0", Published: Day("2026-02-20")}, {Tag: "v1.9.0", Published: Day("2026-01-02")}},
		},
		Snapshots: map[string]map[string]string{
			"ex/action": {"2026-01-01": GoMod("v1.0.0"), "2026-02-03": GoMod("v1.1.0")},
			"ex/late":   {"2026-03-01": GoMod("v1.1.0")},
			"ex/core":   {"2026-01-01": ""},
		},
		Files: map[string]string{
			"ex/action/go.mod@v1.9.0": GoMod("v1.0.0"),
			"ex/action/go.mod@v2.0.0": GoMod("v1.1.0"),
		},
		JSON: map[string]map[string]any{},
	}
}

// Source returns a generated fake whose stubs read w.
func (w *World) Source() *sourcefakes.FakeSource {
	f := &sourcefakes.FakeSource{}
	f.ReleasesStub = func(repo, _ string) ([]source.Release, error) {
		if repo == w.FailRepo {
			return nil, ErrBoom
		}

		return w.Releases[repo], nil
	}
	f.TagCommitStub = w.tagCommit
	f.PathHistoryStub = w.pathHistory
	f.CommitAtStub = func(repo string, asOf time.Time) (string, error) {
		if repo == w.FailRepo {
			return "", ErrBoom
		}

		best := ""
		for d := range w.Snapshots[repo] {
			if !Day(d).After(asOf) && d > best {
				best = d
			}
		}

		if best == "" {
			return "", source.ErrNotFound
		}

		return repo + "@" + best, nil
	}
	f.FileStub = func(repo, path, ref string) (string, bool, error) {
		if repo == w.FailRepo {
			return "", false, ErrBoom
		}

		if c, ok := w.Files[repo+"/"+path+"@"+ref]; ok {
			return c, true, nil
		}

		if path == "go.mod" {
			if i := strings.LastIndex(ref, "@"); i > 0 {
				if c, ok := w.Snapshots[repo][ref[i+1:]]; ok {
					return c, true, nil
				}
			}
		}

		return "", false, nil
	}
	f.GetJSONStub = func(u string) (map[string]any, error) {
		if b, ok := w.JSON[u]; ok {
			return b, nil
		}

		return nil, source.ErrHTTP
	}

	return f
}

func (w *World) tagCommit(repo, tag string) (string, error) {
	if repo == w.FailRepo {
		return "", ErrBoom
	}

	if c, ok := w.Tags[repo+"@"+tag]; ok {
		return c, nil
	}

	return "sha-" + tag, nil
}

// pathHistory treats each go.mod snapshot as a commit, newest first.
func (w *World) pathHistory(repo, path string, until time.Time) ([]source.Commit, error) {
	if repo == w.FailRepo {
		return nil, ErrBoom
	}

	var out []source.Commit

	if path == "go.mod" {
		for d := range w.Snapshots[repo] {
			if !Day(d).After(until) {
				out = append(out, source.Commit{SHA: repo + "@" + d, Date: Day(d)})
			}
		}
	}

	slices.SortFunc(out, func(a, b source.Commit) int { return b.Date.Compare(a.Date) })

	return out, nil
}

// BaseConfig is a baton file for History.
const BaseConfig = `
components:
  core:       {repo: ex/core}
  action:     {repo: ex/action}
  late:       {repo: ex/late, since: 2026-03-01}
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
      - id: late-bumped
        target: late
        action: Bump core to {version}
        verify: {gomod: {module: github.com/example/core}}
`

// WriteFile writes text to a temp file and returns its path.
func WriteFile(t *testing.T, text string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "baton.yml")
	require.NoError(t, os.WriteFile(path, []byte(text), 0o600))

	return path
}

// Config loads text as a baton file, failing the test on error.
func Config(t *testing.T, text string) *config.Config {
	t.Helper()
	cfg, err := config.Load(WriteFile(t, text), probe.Known)
	require.NoError(t, err)

	return cfg
}
