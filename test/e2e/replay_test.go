//go:build e2e

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

// Package e2e replays the Scorecard baton file against real GitHub
// history and checks each case against its known outcome. It needs
// network access and a GitHub token.
package e2e

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/uwu-tools/baton/internal/github"
	"github.com/uwu-tools/baton/pkg/config"
	"github.com/uwu-tools/baton/pkg/probe"
	"github.com/uwu-tools/baton/pkg/reconcile"
)

const file = "../../examples/scorecard/baton.yml"

func TestScorecardReplay(t *testing.T) {
	t.Parallel()

	token, err := github.Token()
	require.NoError(t, err)
	client, err := github.New(token)
	require.NoError(t, err)
	cfg, err := config.Load(file, probe.Known)
	require.NoError(t, err)

	now := time.Now().UTC()

	cases := []struct {
		date, id string
		want     reconcile.State
	}{
		// Known-good: v5.3.0 reached the Action and Allstar releases in a day.
		{"2025-10-05", "scorecard-release/action-released", reconcile.Done},
		{"2025-10-05", "scorecard-release/allstar-released", reconcile.Done},
		{"2025-10-05", "scorecard-release/infra-source-updated", reconcile.NotApplicable},
		// Known-good: v5.6.0 reached the Action, infra, and production.
		{"2026-10-20", "scorecard-release/action-released", reconcile.Done},
		{"2026-10-20", "scorecard-release/infra-released", reconcile.Done},
		// Live only: holds until the next Scorecard release is
		// published and not yet deployed.
		{"", "scorecard-release/infra-deployed", reconcile.Done},
		// Known-bad: v5.5.0 bumped on Action main 06-11, released 07-23
		// (see action#1679).
		{"2026-06-01", "scorecard-release/action-source-updated", reconcile.Overdue},
		{"2026-07-01", "scorecard-release/action-released", reconcile.Overdue},
		{"2026-07-24", "scorecard-release/action-released", reconcile.Done},
		// Known-bad: v5.6.0 hadn't reached Allstar as of 2026-10-20
		// (allstar#905).
		{"2026-10-20", "scorecard-release/allstar-source-updated", reconcile.Overdue},
		{"2026-10-20", "scorecard-release/allstar-released", reconcile.Blocked},
		// A live probe can't be replayed.
		{"2026-10-20", "scorecard-release/infra-deployed", reconcile.Unknown},
	}
	for _, tc := range cases {
		name := tc.date + " " + tc.id
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			asOf := now

			if tc.date != "" {
				d, err := config.ParseDate(tc.date)
				require.NoError(t, err)

				asOf = d.Add(24*time.Hour - time.Second)
			}

			rep, err := reconcile.New(&reconcile.Options{Config: cfg, Source: client, AsOf: asOf, Now: now}).Run()
			require.NoError(t, err)

			for _, r := range rep.All() {
				if r.ID == tc.id {
					require.Equal(t, tc.want, r.State, r.Evidence)

					return
				}
			}

			t.Fatalf("no result for %s", tc.id)
		})
	}

	require.NoError(t, reconcile.CheckInventory(cfg, client, now))
}
