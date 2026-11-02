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

// Package reconcile evaluates every obligation in a baton file.
package reconcile

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"sigs.k8s.io/yaml"

	"github.com/uwu-tools/baton/pkg/config"
	"github.com/uwu-tools/baton/pkg/probe"
	"github.com/uwu-tools/baton/pkg/source"
)

// State of one obligation.
type State string

// Obligation states.
const (
	Done          State = "done"
	Pending       State = "pending"
	Overdue       State = "overdue"
	Blocked       State = "blocked"
	Waived        State = "waived"
	Unknown       State = "unknown"
	NotApplicable State = "not-applicable"
	Error         State = "error"
)

// Result is one evaluated obligation.
type Result struct {
	ID      string   `json:"id"` // "<rollout>/<id>" or invariant id
	Type    string   `json:"type,omitempty"`
	Target  string   `json:"target"`
	Owners  []string `json:"owners,omitempty"`
	Binding string   `json:"binding"`
	Commit  string   `json:"commit,omitempty"` // trigger tag's commit
	Action  string   `json:"action,omitempty"`
	State   State    `json:"state"`
	AgeDays *int     `json:"age_days,omitempty"`
	// DoneSince is when a done step became done, if its probes can tell.
	DoneSince *time.Time `json:"done_since,omitempty"`
	Evidence  string     `json:"evidence"`
}

// Key identifies an obligation instance across runs. A rollout's
// obligations include the trigger commit, so a moved tag gets new keys.
func (r *Result) Key() string {
	if r.Commit == "" {
		return r.ID + "@" + r.Binding
	}

	return r.ID + "@" + r.Binding + ":" + r.Commit
}

// Instance is one rollout triggered by one event.
type Instance struct {
	Rollout  string   `json:"rollout"`
	Key      string   `json:"key"`  // "<repo>:release:<tag>:<commit>"
	Home     string   `json:"home"` // repo that gets the parent issue
	Binding  string   `json:"binding"`
	Commit   string   `json:"commit,omitempty"`
	Complete bool     `json:"complete"`
	Results  []Result `json:"obligations"`
}

// Report is everything one run found.
type Report struct {
	AsOf       time.Time  `json:"as_of"`
	Rules      string     `json:"rules,omitempty"` // digest of the rules file
	Instances  []Instance `json:"rollouts"`
	Invariants []Result   `json:"invariants"`
}

// All returns every obligation result in the report.
func (r *Report) All() []Result {
	var out []Result
	for i := range r.Instances {
		out = append(out, r.Instances[i].Results...)
	}

	return append(out, r.Invariants...)
}

// HasErrors reports whether any obligation couldn't be evaluated.
func (r *Report) HasErrors() bool {
	all := r.All()
	for i := range all {
		if all[i].State == Error {
			return true
		}
	}

	return false
}

// Options configure a reconcile run.
type Options struct {
	Config    *config.Config
	Source    source.Source
	AsOf, Now time.Time
}

// ErrOptions marks unusable reconcile options.
var ErrOptions = errors.New("invalid reconcile options")

// Validate checks that the options can run.
func (o *Options) Validate() error {
	if o.Config == nil || o.Source == nil {
		return fmt.Errorf("%w: config and source are required", ErrOptions)
	}

	if o.AsOf.After(o.Now) {
		return fmt.Errorf("%w: as-of %s is in the future", ErrOptions, o.AsOf.Format(time.DateOnly))
	}

	return nil
}

// Reconciler evaluates obligations.
type Reconciler struct {
	opts *Options
}

// New returns a Reconciler for opts.
func New(opts *Options) *Reconciler {
	return &Reconciler{opts: opts}
}

// Run checks every rollout and invariant as of opts.AsOf.
func (r *Reconciler) Run() (Report, error) {
	if err := r.opts.Validate(); err != nil {
		return Report{}, err
	}

	cfg := r.opts.Config

	rep := Report{AsOf: r.opts.AsOf, Rules: cfg.Digest}
	for i := range cfg.Rollouts {
		rep.Instances = append(rep.Instances, r.rollout(&cfg.Rollouts[i]))
	}

	for i := range cfg.Invariants {
		o := &cfg.Invariants[i]
		target := cfg.Components[o.Target]
		res := newResult(o.ID, o, &target, "always")
		res.Action = o.Action
		r.check(&res, o, "", time.Time{})
		rep.Invariants = append(rep.Invariants, res)
	}

	return rep, nil
}

// waiver returns the active waiver for an obligation instance, if any.
func (r *Reconciler) waiver(id, binding string) *config.Waiver {
	ws := r.opts.Config.Waivers
	for i, w := range ws {
		if w.Obligation != id || (w.Binding != "" && w.Binding != binding) {
			continue
		}

		exp, err := config.ParseDate(w.Expires)
		if err == nil && r.opts.AsOf.Before(exp.Add(24*time.Hour)) {
			return &ws[i]
		}
	}

	return nil
}

// check runs an obligation's probes and classifies the outcome. An
// error in any check wins; a failing check makes the step unmet even if
// another is unknowable; it is unknown only if the rest pass.
func (r *Reconciler) check(res *Result, o *config.Obligation, version string, triggered time.Time) {
	ctx := &probe.Context{
		Source: r.opts.Source, AsOf: r.opts.AsOf, Now: r.opts.Now, Version: version,
		Target: r.opts.Config.Components[o.Target], Components: r.opts.Config.Components,
	}

	done, unknown, sinceKnown := true, false, true
	evidence := make([]string, 0, len(o.Verify))

	var since time.Time

	for _, c := range o.Verify {
		kind, spec := c.Probe()

		out, err := probe.Run(kind, spec, ctx)
		switch {
		case errors.Is(err, probe.ErrUnknowable):
			unknown = true

			evidence = append(evidence, labeled(o, kind, err.Error()))

			continue
		case err != nil:
			res.State, res.Evidence = Error, labeled(o, kind, err.Error())

			return
		}

		done = done && out.Done
		evidence = append(evidence, labeled(o, kind, out.Evidence))

		// The step became done when its last check did.
		sinceKnown = sinceKnown && !out.Since.IsZero()
		if out.Since.After(since) {
			since = out.Since
		}
	}

	res.Evidence = strings.Join(evidence, "; ")

	grace, err := config.ParseGrace(o.Grace)
	if err != nil {
		res.State, res.Evidence = Error, err.Error()

		return
	}

	switch {
	case done && unknown:
		res.State = Unknown

		return
	case done:
		res.State = Done

		if sinceKnown && !since.IsZero() {
			res.DoneSince = &since
		}
	case !triggered.IsZero() && r.opts.AsOf.Sub(triggered) <= grace:
		res.State = Pending
	default:
		res.State = Overdue

		if !triggered.IsZero() {
			days := int(r.opts.AsOf.Sub(triggered).Hours() / 24)
			res.AgeDays = &days
		}
	}
	// A waiver covers an unmet obligation; it never hides an error.
	if res.State == Pending || res.State == Overdue {
		if w := r.waiver(res.ID, res.Binding); w != nil {
			res.State, res.AgeDays = Waived, nil
			res.Evidence = fmt.Sprintf("waived by %s until %s: %s (%s)",
				w.Approver, w.Expires, w.Reason, res.Evidence)
		}
	}
}

// ClockStart is when a step's grace starts: the latest time one of its
// prerequisites became done, or the trigger time when none can say.
// Waived and not-applicable prerequisites count from the trigger.
func ClockStart(triggered time.Time, needs []string, byID map[string]*Result) time.Time {
	start := triggered

	for _, n := range needs {
		dep := byID[n]
		if dep != nil && dep.State == Done && dep.DoneSince != nil && dep.DoneSince.After(start) {
			start = *dep.DoneSince
		}
	}

	return start
}

// labeled prefixes a check's message with its probe kind when the
// obligation has more than one check.
func labeled(o *config.Obligation, kind, msg string) string {
	if len(o.Verify) == 1 {
		return msg
	}

	return kind + ": " + msg
}

func newResult(id string, o *config.Obligation, target *config.Component, binding string) Result {
	return Result{ID: id, Type: o.Type, Target: target.Repo, Owners: target.Owners, Binding: binding}
}

func (r *Reconciler) rollout(ro *config.Rollout) Instance {
	cfg := r.opts.Config
	src := cfg.Components[ro.When.Release]

	in := Instance{Rollout: ro.ID, Home: src.Repo}
	if cfg.Home != nil {
		in.Home = cfg.Home.Repo
	}

	filter := cfg.TriggerFilter(ro.When)

	rel, err := filter.Latest(r.opts.Source, src.Repo, r.opts.AsOf, r.opts.Now)
	if err == nil && rel != nil && rel.Commit == "" {
		rel.Commit, err = r.opts.Source.TagCommit(src.Repo, rel.Tag)
	}

	if err != nil || rel == nil {
		state, evidence := NotApplicable, "no trigger release yet"

		switch {
		case errors.Is(err, source.ErrUnknowable):
			state, evidence = Unknown, err.Error()
		case err != nil:
			state, evidence = Error, err.Error()
		}

		for i := range ro.Obligations {
			o := &ro.Obligations[i]
			target := cfg.Components[o.Target]
			res := newResult(ro.ID+"/"+o.ID, o, &target, "")
			res.State, res.Evidence = state, evidence
			in.Results = append(in.Results, res)
		}

		return in
	}

	in.Binding = ro.When.Release + "@" + rel.Tag
	in.Commit = rel.Commit
	in.Key = src.Repo + ":release:" + rel.Tag + ":" + rel.Commit

	byID := map[string]*Result{}

	defs := map[string]*config.Obligation{}
	for i := range ro.Obligations {
		defs[ro.Obligations[i].ID] = &ro.Obligations[i]
	}

	var run func(id string) *Result

	run = func(id string) *Result {
		if res, ok := byID[id]; ok {
			return res
		}

		o := defs[id]
		target := cfg.Components[o.Target]
		res := newResult(ro.ID+"/"+id, o, &target, in.Binding)
		res.Commit = rel.Commit
		res.Action = strings.ReplaceAll(o.Action, "{version}", rel.Tag)
		byID[id] = &res

		if target.Since != "" {
			since, err := config.ParseDate(target.Since)
			if err == nil && rel.Published.Before(since) {
				res.State = NotApplicable
				res.Evidence = fmt.Sprintf("%s didn't exist until %s", o.Target, target.Since)

				return &res
			}
		}

		if blocked := r.needs(&res, o, run); blocked {
			return &res
		}

		r.check(&res, o, rel.Tag, ClockStart(rel.Published, o.Needs, byID))

		return &res
	}

	in.Complete = true

	for i := range ro.Obligations {
		res := run(ro.Obligations[i].ID)

		in.Results = append(in.Results, *res)
		switch res.State {
		case Done, Waived, NotApplicable:
		default:
			in.Complete = false
		}
	}

	return in
}

// needs settles res from its prerequisites. It reports true when res
// is decided without running its probe: blocked, or waived because a
// prerequisite is waived and this step can't be done without it.
func (r *Reconciler) needs(res *Result, o *config.Obligation, run func(string) *Result) bool {
	waivedBy := ""

	for _, n := range o.Needs {
		dep := run(n)
		switch dep.State {
		case Done, NotApplicable:
		case Waived:
			waivedBy = n
		default:
			res.State = Blocked
			res.Evidence = fmt.Sprintf("needs %s (%s)", n, dep.State)

			return true
		}
	}

	if waivedBy != "" {
		res.State = Waived
		res.Evidence = "inherits the waiver on " + waivedBy

		return true
	}

	return false
}

// ErrInventory marks a component missing from the project inventory.
var ErrInventory = errors.New("inventory check failed")

// CheckInventory reports components missing from the Security
// Insights repository list. It reads the current file, not a past one.
func CheckInventory(cfg *config.Config, src source.Source, now time.Time) error {
	inv := cfg.Inventory
	if inv == nil {
		return nil
	}

	repo := cfg.Components[inv.Component].Repo

	sha, err := src.CommitAt(repo, now)
	if err != nil {
		return fmt.Errorf("inventory: %w", err)
	}

	text, found, err := src.File(repo, inv.Path, sha)
	if err != nil {
		return fmt.Errorf("inventory: %w", err)
	}

	if !found {
		return fmt.Errorf("%w: %s/%s not found", ErrInventory, repo, inv.Path)
	}

	var si struct {
		Project struct {
			Repositories []struct {
				URL string `json:"url"`
			} `json:"repositories"`
		} `json:"project"`
	}
	if err := yaml.Unmarshal([]byte(text), &si); err != nil {
		return fmt.Errorf("%w: parse %s: %w", ErrInventory, inv.Path, err)
	}

	listed := map[string]bool{}
	for _, r := range si.Project.Repositories {
		listed[strings.TrimPrefix(strings.TrimSuffix(r.URL, "/"), "https://github.com/")] = true
	}

	if len(listed) == 0 {
		return fmt.Errorf("%w: %s lists no project.repositories", ErrInventory, inv.Path)
	}

	var missing []string

	for name, c := range cfg.Components {
		if !listed[c.Repo] {
			missing = append(missing, fmt.Sprintf("%s (%s)", name, c.Repo))
		}
	}

	if len(missing) > 0 {
		return fmt.Errorf("%w: not listed in %s: %s", ErrInventory, inv.Path, strings.Join(missing, ", "))
	}

	return nil
}
