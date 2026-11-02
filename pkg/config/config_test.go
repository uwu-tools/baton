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

package config_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/uwu-tools/baton/internal/batontest"
	"github.com/uwu-tools/baton/pkg/config"
	"github.com/uwu-tools/baton/pkg/probe"
)

func TestLoadValid(t *testing.T) {
	t.Parallel()
	cfg, err := config.Load(batontest.WriteFile(t, batontest.BaseConfig), probe.Known)
	require.NoError(t, err)
	require.Len(t, cfg.Rollouts, 1)
	require.Len(t, cfg.Rollouts[0].Obligations, 3)
	require.Len(t, cfg.Digest, 12)
}

func TestLoadCheckList(t *testing.T) {
	t.Parallel()
	cfg, err := config.Load(batontest.WriteFile(t, strings.Replace(batontest.BaseConfig,
		"verify: {release_gomod: {module: github.com/example/core}}",
		"verify:\n"+
			"          - release_gomod: {module: github.com/example/core}\n"+
			"          - gomod: {module: github.com/example/core}", 1)),
		probe.Known)
	require.NoError(t, err)

	checks := cfg.Rollouts[0].Obligations[1].Verify
	require.Len(t, checks, 2)

	kind, spec := checks[1].Probe()
	require.Equal(t, "gomod", kind)
	require.Equal(t, "github.com/example/core", spec["module"])

	require.Len(t, cfg.Rollouts[0].Obligations[0].Verify, 1, "a single mapping is one check")
}

func TestDigestFollowsContent(t *testing.T) {
	t.Parallel()

	load := func(text string) string {
		cfg, err := config.Load(batontest.WriteFile(t, text), probe.Known)
		require.NoError(t, err)

		return cfg.Digest
	}

	base := load(batontest.BaseConfig)
	require.Equal(t, base, load(batontest.BaseConfig), "same file, same digest")
	require.NotEqual(t, base, load(batontest.BaseConfig+"\n# edited\n"), "any edit changes it")
}

func TestLoadInvalid(t *testing.T) {
	t.Parallel()

	base := batontest.BaseConfig
	for name, tc := range map[string]struct{ config, want string }{
		"unknown key":    {strings.Replace(base, "grace: 7d", "grase: 7d", 1), `unknown field "grase"`},
		"unknown target": {strings.Replace(base, "target: late", "target: nope", 1), `unknown target "nope"`},
		"unknown needs": {
			strings.Replace(base, "needs: [action-bumped]", "needs: [nope]", 1),
			`needs unknown obligation "nope"`,
		},
		"cycle": {
			strings.Replace(strings.Replace(base, "needs: [action-bumped]", "needs: [late-bumped]", 1),
				"target: late\n", "target: late\n        needs: [action-released]\n", 1),
			"needs cycle",
		},
		"bad grace":     {strings.Replace(base, "grace: 7d", "grace: 1w", 1), "grace must look like"},
		"unknown probe": {strings.Replace(base, "verify: {gomod:", "verify: {gomodd:", 1), `unknown probe "gomodd"`},
		"two probes in one check": {
			strings.Replace(base, "verify: {release_gomod: {module: github.com/example/core}}",
				"verify: {release_gomod: {module: m}, gomod: {module: m}}", 1),
			"verify check 1 must name exactly one probe",
		},
		"empty check list": {strings.Replace(base, "verify: {release_gomod: {module: github.com/example/core}}",
			"verify: []", 1), "verify needs at least one check"},
		"unknown probe in list": {
			strings.Replace(base, "verify: {release_gomod: {module: github.com/example/core}}",
				"verify: [{gomod: {module: m}}, {nope: {}}]", 1),
			`unknown probe "nope"`,
		},
		"check that isn't a mapping": {
			strings.Replace(base, "verify: {release_gomod: {module: github.com/example/core}}",
				"verify: [gomod]", 1),
			"verify:",
		},
		"bad source": {
			strings.Replace(base, "{repo: ex/core}", "{repo: ex/core, source: branches}", 1),
			"source must be releases or tags",
		},
		"bad order": {
			strings.Replace(base, "{repo: ex/core}", "{repo: ex/core, releases: {order: newest}}", 1),
			"releases.order must be",
		},
		"forge order on tags": {
			strings.Replace(base, "{repo: ex/core}", "{repo: ex/core, source: tags, releases: {order: forge}}", 1),
			"forge needs source releases",
		},
		"bad release pattern": {
			strings.Replace(base, "{repo: ex/core}", "{repo: ex/core, releases: {tag: '('}}", 1),
			"releases.tag:",
		},
		"bad trigger pattern": {
			strings.Replace(base, "when: {release: core}", "when: {release: core, tag: '('}", 1),
			"when.tag:",
		},
		"bad level": {
			strings.Replace(base, "when: {release: core}", "when: {release: core, level: tiny}", 1),
			"when.level must be",
		},
		"level without version order": {
			strings.Replace(strings.Replace(base, "when: {release: core}", "when: {release: core, level: minor}", 1),
				"{repo: ex/core}", "{repo: ex/core, releases: {order: published}}", 1),
			"when.level needs",
		},
		"waiver without approver": {base + `
waivers:
  - obligation: core-release/action-bumped
    reason: x
    expires: 2026-01-01
`, "approver and reason are required"},
		"waiver for unknown obligation": {base + `
waivers:
  - obligation: core-release/nope
    approver: a
    reason: x
    expires: 2026-01-01
`, "unknown obligation"},
		"bad since": {strings.Replace(base, "since: 2026-03-01", "since: March", 1), "since:"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := config.Load(batontest.WriteFile(t, tc.config), probe.Known)
			require.ErrorContains(t, err, tc.want)
		})
	}
}

func TestParseGrace(t *testing.T) {
	t.Parallel()

	d, err := config.ParseGrace("7d")
	require.NoError(t, err)
	require.Equal(t, 7*24, int(d.Hours()))

	_, err = config.ParseGrace("-1d")
	require.ErrorIs(t, err, config.ErrInvalid)
}
