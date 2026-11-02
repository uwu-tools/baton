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
	"errors"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/uwu-tools/baton/pkg/plan"
	"github.com/uwu-tools/baton/pkg/writer"
)

func newApply(clients ClientFactory, now func() time.Time) *cobra.Command {
	opts := &writer.Options{}

	var asJSON bool

	c := &cobra.Command{
		Use:   "apply FILE",
		Short: "Open, update, and close issues to match the plan",
		Long: "Evaluate every obligation as of now and make the issue changes plan prints.\n" +
			"The preview writes only with --redirect-owner, which sends every issue to a\n" +
			"sandbox owner and drops assignees.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.Validate(); err != nil {
				return fmt.Errorf("apply: %w", err)
			}

			cfg, err := loadConfig(args[0])
			if err != nil {
				return err
			}

			cl, err := clients()
			if err != nil {
				return err
			}

			t := now()

			rep, evalErr := evaluate(cfg, cl, t, t)
			if rep == nil {
				return evalErr
			}

			items := plan.Plan(rep)
			if err := report(cmd.OutOrStdout(), rep, items, asJSON); err != nil {
				return err
			}

			log, err := writer.New(opts, cl.Issues).Run(rep, items)

			p := &printer{w: cmd.OutOrStdout()}
			for _, l := range log {
				p.f("  apply: %s\n", l)
			}

			if err != nil {
				return fmt.Errorf("%w: apply: %w", ErrEvaluation, err)
			}

			return errors.Join(p.err, evalErr)
		},
	}
	c.Flags().StringVar(&opts.RedirectOwner, "redirect-owner", "",
		"write issues for `owner`/name to this owner instead, without assignees (required)")
	c.Flags().BoolVar(&opts.CreateLabel, "create-label", false, "create the baton label where it's missing")
	c.Flags().BoolVar(&asJSON, "json", false, "print the report and plan as JSON")

	return c
}
