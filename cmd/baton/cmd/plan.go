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
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"

	"github.com/uwu-tools/baton/pkg/config"
	"github.com/uwu-tools/baton/pkg/plan"
	"github.com/uwu-tools/baton/pkg/reconcile"
)

type planOptions struct {
	asOf   string
	asJSON bool
}

func newPlan(clients ClientFactory, now func() time.Time) *cobra.Command {
	opts := &planOptions{}
	c := &cobra.Command{
		Use:   "plan FILE",
		Short: "Evaluate every obligation and print the issue changes",
		Long: "Evaluate every rollout and invariant, as of now or a past date, and print each\n" +
			"obligation's state and the issue changes apply would make. Read-only.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig(args[0])
			if err != nil {
				return err
			}

			cl, err := clients()
			if err != nil {
				return err
			}

			asOf, err := resolveAsOf(opts.asOf, now())
			if err != nil {
				return err
			}

			rep, evalErr := evaluate(cfg, cl, asOf, now())
			if rep == nil {
				return evalErr
			}

			if err := report(cmd.OutOrStdout(), rep, plan.Plan(rep), opts.asJSON); err != nil {
				return err
			}

			return evalErr
		},
	}
	c.Flags().StringVar(&opts.asOf, "as-of", "", "evaluate as of `YYYY-MM-DD` (end of day UTC); default now")
	c.Flags().BoolVar(&opts.asJSON, "json", false, "print the report and plan as JSON")

	return c
}

func resolveAsOf(flag string, now time.Time) (time.Time, error) {
	if flag == "" {
		return now, nil
	}

	d, err := config.ParseDate(flag)
	if err != nil {
		return time.Time{}, fmt.Errorf("--as-of: %w", err)
	}

	if end := d.Add(24*time.Hour - time.Second); end.Before(now) {
		return end, nil
	}

	return now, nil
}

var errObligations = errors.New("one or more obligations couldn't be evaluated")

// evaluate runs the inventory check and the reconciler. It returns the
// report when one was produced, and ErrEvaluation if anything failed.
func evaluate(cfg *config.Config, cl *Clients, asOf, now time.Time) (*reconcile.Report, error) {
	var errs []error

	if err := reconcile.CheckInventory(cfg, cl.Source, now); err != nil {
		logrus.Error(err)
		errs = append(errs, err)
	}

	rep, err := reconcile.New(&reconcile.Options{Config: cfg, Source: cl.Source, AsOf: asOf, Now: now}).Run()
	if err != nil {
		return nil, fmt.Errorf("reconcile: %w", err)
	}

	if rep.HasErrors() {
		errs = append(errs, errObligations)
	}

	if len(errs) > 0 {
		return &rep, fmt.Errorf("%w: %w", ErrEvaluation, errors.Join(errs...))
	}

	return &rep, nil
}

func report(w io.Writer, rep *reconcile.Report, items []plan.Item, asJSON bool) error {
	if asJSON {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")

		if err := enc.Encode(map[string]any{"report": rep, "plan": items}); err != nil {
			return fmt.Errorf("encode report: %w", err)
		}

		return nil
	}

	p := &printer{w: w}
	p.f("as of %s\n", rep.AsOf.Format(time.DateOnly))

	line := func(r *reconcile.Result) {
		age := ""
		if r.AgeDays != nil {
			age = fmt.Sprintf("(%dd)", *r.AgeDays)
		}

		p.f("    %-14s %-6s %-42s %s\n", r.State, age, r.ID, r.Evidence)
	}

	for i := range rep.Instances {
		in := &rep.Instances[i]
		status := "in progress"

		switch {
		case in.Key == "":
			status = "not triggered"
		case in.Complete:
			status = "complete"
		}

		p.f("  rollout %s %s: %s\n", in.Rollout, in.Binding, status)

		for j := range in.Results {
			line(&in.Results[j])
		}
	}

	if len(rep.Invariants) > 0 {
		p.f("  invariants\n")

		for i := range rep.Invariants {
			line(&rep.Invariants[i])
		}
	}

	for i := range items {
		it := &items[i]
		p.f("  plan: %-14s %-22s [%s] %s\n", it.Op, it.Repo, it.Key, it.Title)
	}

	return p.err
}
