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

package cmd

import (
	"github.com/spf13/cobra"
)

func newValidate() *cobra.Command {
	return &cobra.Command{
		Use:   "validate FILE",
		Short: "Check a baton file offline",
		Long:  "Check a baton file's syntax and references without calling GitHub. Suitable for pull requests.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig(args[0])
			if err != nil {
				return err
			}

			p := &printer{w: cmd.OutOrStdout()}
			p.f("%s: %d rollout(s), %d invariant(s), %d waiver(s): ok\n",
				args[0], len(cfg.Rollouts), len(cfg.Invariants), len(cfg.Waivers))

			return p.err
		},
	}
}
