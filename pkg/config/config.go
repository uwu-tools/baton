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

// Package config loads and validates a baton file.
package config

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"sigs.k8s.io/yaml"

	"github.com/uwu-tools/baton/pkg/source"
)

// Config is a baton file.
type Config struct {
	Home       *Home                `json:"home,omitempty"`
	Inventory  *Inventory           `json:"inventory,omitempty"`
	Components map[string]Component `json:"components"`
	Rollouts   []Rollout            `json:"rollouts,omitempty"`
	Invariants []Obligation         `json:"invariants,omitempty"`
	Waivers    []Waiver             `json:"waivers,omitempty"`

	// Digest is the first 12 hex digits of the file's SHA-256, set by
	// Load, so issues can show which rules produced them.
	Digest string `json:"-"`
}

// Home is the project's baton repo, which holds the parent rollout
// issues. Without it, parents go to the triggering repo.
type Home struct {
	Repo string `json:"repo"`
}

// Inventory points at the Security Insights file that lists the
// project's repositories. Every component must appear in it.
type Inventory struct {
	Component string `json:"component"`
	Path      string `json:"path"`
}

// Component is one repository in the project.
type Component struct {
	Repo     string    `json:"repo"`
	Source   string    `json:"source,omitempty"`   // "releases" (default) or "tags"
	Releases *Releases `json:"releases,omitempty"` // what counts as a release
	Since    string    `json:"since,omitempty"`    // YYYY-MM-DD the component started
	Owners   []string  `json:"owners,omitempty"`   // optional GitHub users or teams
}

// Releases says which of a component's tags count as releases.
type Releases struct {
	Tag        string `json:"tag,omitempty"`        // RE2 pattern the tag must match
	Prerelease bool   `json:"prerelease,omitempty"` // count prereleases
	Order      string `json:"order,omitempty"`      // version (default), published, or forge
}

// Filter returns the component's release filter. Call it only on a
// validated config, whose patterns are known to compile.
func (c *Component) Filter() source.Filter {
	f := source.Filter{Kind: c.SourceKind(), Order: source.OrderVersion}
	if r := c.Releases; r != nil {
		f.Prerelease = r.Prerelease
		if r.Order != "" {
			f.Order = r.Order
		}

		if r.Tag != "" {
			f.Tags = append(f.Tags, regexp.MustCompile(r.Tag))
		}
	}

	return f
}

// SourceKind reports where the component's versions come from.
func (c *Component) SourceKind() string {
	if c.Source == "" {
		return source.KindReleases
	}

	return c.Source
}

// Rollout is one trigger and the obligations it creates.
type Rollout struct {
	ID          string       `json:"id"`
	When        Trigger      `json:"when"`
	Obligations []Obligation `json:"obligations"`
}

// Trigger is {release: <component>}. More event kinds come later.
type Trigger struct {
	Release string `json:"release"`
	Level   string `json:"level,omitempty"` // major, minor, or patch (default)
	Tag     string `json:"tag,omitempty"`   // RE2 pattern, on top of the component's
}

// TriggerFilter returns the filter for releases that trigger a rollout:
// the source component's filter, narrowed by the trigger.
func (c *Config) TriggerFilter(t Trigger) source.Filter {
	comp := c.Components[t.Release]
	f := comp.Filter()
	f.Level = t.Level

	if t.Tag != "" {
		f.Tags = append(f.Tags, regexp.MustCompile(t.Tag))
	}

	return f
}

// Obligation is an action owed by Target, verified by one probe.
type Obligation struct {
	ID     string   `json:"id"`
	Type   string   `json:"type,omitempty"` // catalog type, for reporting
	Target string   `json:"target"`
	Needs  []string `json:"needs,omitempty"`
	Action string   `json:"action"`
	Verify Checks   `json:"verify"`
	Grace  string   `json:"grace,omitempty"`
}

// Check is one probe: a mapping with exactly one key, the probe kind.
type Check map[string]map[string]any

// Probe returns the check's probe kind and spec.
func (c Check) Probe() (kind string, spec map[string]any) {
	for k, s := range c {
		return k, s
	}

	return "", nil
}

// Checks are an obligation's probes, all of which must pass. The file
// takes one check as a mapping or several as a list.
type Checks []Check

// UnmarshalJSON accepts a single check or a list of checks.
func (c *Checks) UnmarshalJSON(data []byte) error {
	if trimmed := bytes.TrimSpace(data); len(trimmed) > 0 && trimmed[0] == '[' {
		var list []Check
		if err := json.Unmarshal(data, &list); err != nil {
			return fmt.Errorf("verify: %w", err)
		}

		*c = list

		return nil
	}

	var one Check
	if err := json.Unmarshal(data, &one); err != nil {
		return fmt.Errorf("verify: %w", err)
	}

	*c = Checks{one}

	return nil
}

// Waiver excuses one obligation, optionally for one binding, until it
// expires. It lives in the file so it goes through review.
type Waiver struct {
	Obligation string `json:"obligation"`        // "<rollout>/<id>" or invariant id
	Binding    string `json:"binding,omitempty"` // e.g. "scorecard@v5.6.0"
	Approver   string `json:"approver"`
	Reason     string `json:"reason"`
	Expires    string `json:"expires"` // YYYY-MM-DD
}

const digestLen = 12

// Load reads and validates a baton file. isProbe reports whether a
// verify kind exists. Unknown keys are errors, so a misspelled key
// can't silently disable a rule.
func Load(path string, isProbe func(string) bool) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}

	var cfg Config
	if err := yaml.UnmarshalStrict(data, &cfg); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}

	if err := cfg.Validate(isProbe); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}

	sum := sha256.Sum256(data)
	cfg.Digest = hex.EncodeToString(sum[:])[:digestLen]

	return &cfg, nil
}

// ParseGrace parses a grace period such as "7d".
func ParseGrace(s string) (time.Duration, error) {
	if s == "" {
		return 0, nil
	}

	n, err := strconv.Atoi(strings.TrimSuffix(s, "d"))
	if err != nil || !strings.HasSuffix(s, "d") || n < 0 {
		return 0, fmt.Errorf("%w: grace must look like \"7d\", got %q", ErrInvalid, s)
	}

	return time.Duration(n) * 24 * time.Hour, nil
}

// ParseDate parses a YYYY-MM-DD date as UTC midnight.
func ParseDate(s string) (time.Time, error) {
	t, err := time.Parse(time.DateOnly, s)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse date: %w", err)
	}

	return t, nil
}

// ErrInvalid marks a configuration error.
var ErrInvalid = errors.New("invalid config")

// Validate checks references and cycles so a typo fails loudly
// instead of silently skipping an obligation.
func (c *Config) Validate(isProbe func(string) bool) error {
	v := &validator{cfg: c, isProbe: isProbe, known: map[string]bool{}}
	v.components()
	v.rollouts()
	v.invariants()
	v.waivers()

	return errors.Join(v.errs...)
}

type validator struct {
	cfg     *Config
	isProbe func(string) bool
	known   map[string]bool
	errs    []error
}

func (v *validator) addf(format string, a ...any) {
	v.errs = append(v.errs, fmt.Errorf("%w: %s", ErrInvalid, fmt.Sprintf(format, a...)))
}

// releases checks a component's source and release filter.
func (v *validator) releases(name string, comp *Component) {
	kind := comp.SourceKind()
	if kind != source.KindReleases && kind != source.KindTags {
		v.addf("component %q: source must be releases or tags, not %q", name, kind)
	}

	r := comp.Releases
	if r == nil {
		return
	}

	if r.Tag != "" {
		if _, err := regexp.Compile(r.Tag); err != nil {
			v.addf("component %q: releases.tag: %v", name, err)
		}
	}

	switch r.Order {
	case "", source.OrderVersion, source.OrderPublished:
	case source.OrderForge:
		if kind != source.KindReleases {
			v.addf("component %q: releases.order forge needs source releases; tags have no latest flag", name)
		}
	default:
		v.addf("component %q: releases.order must be version, published, or forge, not %q", name, r.Order)
	}
}

func (v *validator) components() {
	for name, comp := range v.cfg.Components {
		if comp.Repo == "" {
			v.addf("component %q: missing repo", name)
		}

		if comp.Since != "" {
			if _, err := ParseDate(comp.Since); err != nil {
				v.addf("component %q: since: %v", name, err)
			}
		}

		v.releases(name, &comp)
	}

	if v.cfg.Home != nil && v.cfg.Home.Repo == "" {
		v.addf("home: missing repo")
	}

	if inv := v.cfg.Inventory; inv != nil {
		if _, ok := v.cfg.Components[inv.Component]; !ok {
			v.addf("inventory: unknown component %q", inv.Component)
		}
	}
}

func (v *validator) obligation(where string, o *Obligation) {
	if _, ok := v.cfg.Components[o.Target]; !ok {
		v.addf("%s: unknown target %q", where, o.Target)
	}

	if o.Action == "" {
		v.addf("%s: missing action", where)
	}

	if len(o.Verify) == 0 {
		v.addf("%s: verify needs at least one check", where)
	}

	for i, c := range o.Verify {
		if len(c) != 1 {
			v.addf("%s: verify check %d must name exactly one probe; list several checks instead", where, i+1)
		}

		for kind := range c {
			if !v.isProbe(kind) {
				v.addf("%s: unknown probe %q", where, kind)
			}
		}
	}

	if _, err := ParseGrace(o.Grace); err != nil {
		v.addf("%s: %v", where, err)
	}
}

// trigger checks a rollout's when block.
func (v *validator) trigger(id string, t Trigger) {
	comp, ok := v.cfg.Components[t.Release]
	if !ok {
		v.addf("rollout %q: when.release: unknown component %q", id, t.Release)
	}

	if t.Tag != "" {
		if _, err := regexp.Compile(t.Tag); err != nil {
			v.addf("rollout %q: when.tag: %v", id, err)
		}
	}

	switch t.Level {
	case "", source.LevelPatch:
	case source.LevelMinor, source.LevelMajor:
		if ok && comp.Releases != nil && comp.Releases.Order != "" && comp.Releases.Order != source.OrderVersion {
			v.addf("rollout %q: when.level needs the %s component's releases ordered by version", id, t.Release)
		}
	default:
		v.addf("rollout %q: when.level must be major, minor, or patch, not %q", id, t.Level)
	}
}

func (v *validator) rollouts() {
	seen := map[string]bool{}
	for _, ro := range v.cfg.Rollouts {
		if seen[ro.ID] {
			v.addf("rollout %q: duplicate id", ro.ID)
		}

		seen[ro.ID] = true
		v.trigger(ro.ID, ro.When)

		ids := map[string]bool{}

		for i := range ro.Obligations {
			o := &ro.Obligations[i]
			if ids[o.ID] {
				v.addf("rollout %q: duplicate obligation %q", ro.ID, o.ID)
			}

			ids[o.ID] = true
			v.known[ro.ID+"/"+o.ID] = true
		}

		needs := map[string][]string{}

		for i := range ro.Obligations {
			o := &ro.Obligations[i]
			where := fmt.Sprintf("rollout %q obligation %q", ro.ID, o.ID)
			v.obligation(where, o)

			for _, n := range o.Needs {
				if !ids[n] {
					v.addf("%s: needs unknown obligation %q", where, n)
				}
			}

			needs[o.ID] = o.Needs
		}

		if cyc := findCycle(needs); cyc != "" {
			v.addf("rollout %q: needs cycle through %q", ro.ID, cyc)
		}
	}
}

func (v *validator) invariants() {
	for i := range v.cfg.Invariants {
		o := &v.cfg.Invariants[i]

		where := fmt.Sprintf("invariant %q", o.ID)
		if v.known[o.ID] {
			v.addf("%s: duplicate id", where)
		}

		v.known[o.ID] = true
		v.obligation(where, o)

		if len(o.Needs) > 0 {
			v.addf("%s: invariants can't have needs", where)
		}
	}
}

func (v *validator) waivers() {
	for i, w := range v.cfg.Waivers {
		where := fmt.Sprintf("waiver %d (%s)", i+1, w.Obligation)
		if !v.known[w.Obligation] {
			v.addf("%s: unknown obligation", where)
		}

		if w.Approver == "" || w.Reason == "" {
			v.addf("%s: approver and reason are required", where)
		}

		if _, err := ParseDate(w.Expires); err != nil {
			v.addf("%s: expires: %v", where, err)
		}
	}
}

// findCycle returns a node on a cycle in the needs graph, or "".
func findCycle(needs map[string][]string) string {
	const (
		visiting = 1
		finished = 2
	)

	mark := map[string]int{}

	var visit func(n string) string

	visit = func(n string) string {
		switch mark[n] {
		case visiting:
			return n
		case finished:
			return ""
		}

		mark[n] = visiting
		for _, d := range needs[n] {
			if c := visit(d); c != "" {
				return c
			}
		}

		mark[n] = finished

		return ""
	}
	for n := range needs {
		if c := visit(n); c != "" {
			return c
		}
	}

	return ""
}
