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

// Package probe checks whether an owed action is done, as of a date.
package probe

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"golang.org/x/mod/modfile"
	"golang.org/x/mod/semver"

	"github.com/uwu-tools/baton/pkg/config"
	"github.com/uwu-tools/baton/pkg/source"
)

var (
	// ErrUnknowable means a probe can't answer for the requested date by
	// design, such as a live endpoint during a replay. It isn't a failure.
	ErrUnknowable = source.ErrUnknowable

	// ErrProbe marks a probe that couldn't answer. It's never reported
	// as "not done".
	ErrProbe = errors.New("probe failed")
)

// Context is what a probe needs to answer one obligation.
type Context struct {
	Source     source.Source
	AsOf, Now  time.Time
	Version    string // bound by the trigger; empty for invariants
	Target     config.Component
	Components map[string]config.Component
}

// Result is a probe's answer. Since is when the fact became true, for
// probes that can tell; zero means not known.
type Result struct {
	Done     bool
	Evidence string
	Since    time.Time
}

type probeFunc func(spec map[string]any, c *Context) (Result, error)

var probes = map[string]probeFunc{
	"gomod":          goMod,
	"release_gomod":  releaseGoMod,
	"deployed_gomod": deployedGoMod,
	"files_equal":    filesEqual,
	"file_lacks":     fileLacks,
}

// Known reports whether kind names a probe.
func Known(kind string) bool {
	_, ok := probes[kind]

	return ok
}

// Run evaluates the probe named kind.
func Run(kind string, spec map[string]any, c *Context) (Result, error) {
	p, ok := probes[kind]
	if !ok {
		return Result{}, fmt.Errorf("%w: unknown probe %q", ErrProbe, kind)
	}

	return p(spec, c)
}

func str(spec map[string]any, key, def string) string {
	if v, ok := spec[key].(string); ok && v != "" {
		return v
	}

	return def
}

func short(ref string) string {
	if len(ref) == 40 {
		return ref[:12]
	}

	return ref
}

// requiredVersion reads the version of module that go.mod at ref requires.
func requiredVersion(c *Context, repo, path, ref, module string) (string, error) {
	text, found, err := c.Source.File(repo, path, ref)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", path, err)
	}

	if !found {
		return "", fmt.Errorf("%w: %s: no %s at %s", ErrProbe, repo, path, short(ref))
	}

	f, err := modfile.ParseLax(path, []byte(text), nil)
	if err != nil {
		return "", fmt.Errorf("%w: %s@%s: parse %s: %w", ErrProbe, repo, short(ref), path, err)
	}

	for _, r := range f.Require {
		if r.Mod.Path == module {
			return r.Mod.Version, nil
		}
	}

	return "", fmt.Errorf("%w: %s@%s: %s doesn't require %s", ErrProbe, repo, short(ref), path, module)
}

// atLeast compares module versions. A pseudo-version for a later commit
// (v5.6.1-0.2026…) counts as newer than the release it builds on.
func atLeast(have, want string) (bool, error) {
	if !semver.IsValid(have) || !semver.IsValid(want) {
		return false, fmt.Errorf("%w: can't compare versions %q and %q", ErrProbe, have, want)
	}

	return semver.Compare(have, want) >= 0, nil
}

func goMod(spec map[string]any, c *Context) (Result, error) {
	repo := c.Target.Repo

	sha, err := c.Source.CommitAt(repo, c.AsOf)
	if err != nil {
		return Result{}, fmt.Errorf("commit of %s: %w", repo, err)
	}

	have, err := requiredVersion(c, repo, str(spec, "path", "go.mod"), sha, str(spec, "module", ""))
	if err != nil {
		return Result{}, err
	}

	ok, err := atLeast(have, c.Version)
	if err != nil || !ok {
		return Result{Done: ok, Evidence: fmt.Sprintf("%s@%s requires %s", repo, short(sha), have)}, err
	}

	since, err := goModSince(c, repo, str(spec, "path", "go.mod"), str(spec, "module", ""))

	return Result{Done: true, Evidence: fmt.Sprintf("%s@%s requires %s", repo, short(sha), have), Since: since}, err
}

// satisfies reports whether path at an older ref requires module at
// the bound version, for walking history. A go.mod that doesn't parse,
// doesn't require module, or names an invalid version ends the walk;
// only a failed read is an error.
func satisfies(c *Context, repo, path, ref, module string) (bool, error) {
	have, err := requiredVersion(c, repo, path, ref, module)
	if errors.Is(err, ErrProbe) {
		return false, nil
	}

	if err != nil {
		return false, err
	}

	return semver.IsValid(have) && semver.Compare(have, c.Version) >= 0, nil
}

// goModSince walks back through the commits that changed path and
// returns the date of the oldest one in the unbroken run that requires
// module at the bound version.
func goModSince(c *Context, repo, path, module string) (time.Time, error) {
	commits, err := c.Source.PathHistory(repo, path, c.AsOf)
	if err != nil {
		return time.Time{}, fmt.Errorf("history of %s: %w", path, err)
	}

	var since time.Time

	for _, cm := range commits {
		ok, err := satisfies(c, repo, path, cm.SHA, module)
		if err != nil {
			return time.Time{}, err
		}

		if !ok {
			break
		}

		since = cm.Date
	}

	return since, nil
}

func releaseGoMod(spec map[string]any, c *Context) (Result, error) {
	repo, path, module := c.Target.Repo, str(spec, "path", "go.mod"), str(spec, "module", "")

	f := c.Target.Filter()
	f.Kind = str(spec, "source", f.Kind)

	rels, err := f.Releases(c.Source, repo, c.AsOf, c.Now)
	if err != nil {
		return Result{}, fmt.Errorf("latest release: %w", err)
	}

	if len(rels) == 0 {
		return Result{Done: false, Evidence: repo + ": no release yet"}, nil
	}

	latest := rels[0]

	have, err := requiredVersion(c, repo, path, latest.Tag, module)
	if err != nil {
		return Result{}, err
	}

	ok, err := atLeast(have, c.Version)
	res := Result{Done: ok, Evidence: fmt.Sprintf("%s %s requires %s", repo, latest.Tag, have)}

	if err != nil || !ok {
		return res, err
	}

	// Walk down from the latest to the oldest release in the unbroken
	// run that satisfies the bound version.
	for _, r := range rels {
		ok, err := satisfies(c, repo, path, r.Tag, module)
		if err != nil {
			return Result{}, err
		}

		if !ok {
			break
		}

		res.Since = r.Published
	}

	return res, nil
}

func deployedGoMod(spec map[string]any, c *Context) (Result, error) {
	if c.AsOf.Before(c.Now.Add(-time.Hour)) {
		return Result{}, ErrUnknowable
	}

	u := str(spec, "url", "")

	body, err := c.Source.GetJSON(u)
	if err != nil {
		return Result{}, fmt.Errorf("deployed version: %w", err)
	}

	field := str(spec, "field", "commit")

	commit, ok := body[field].(string)
	if !ok || commit == "" {
		return Result{}, fmt.Errorf("%w: %s: no %q field", ErrProbe, u, field)
	}

	have, err := requiredVersion(c, c.Target.Repo, str(spec, "path", "go.mod"), commit, str(spec, "module", ""))
	if err != nil {
		return Result{}, err
	}

	done, err := atLeast(have, c.Version)

	return Result{Done: done, Evidence: fmt.Sprintf("deployed %s requires %s", short(commit), have)}, err
}

func filesEqual(spec map[string]any, c *Context) (Result, error) {
	var texts [2]string

	for i, side := range []string{"source", "copy"} {
		s, ok := spec[side].(map[string]any)
		if !ok {
			return Result{}, fmt.Errorf("%w: files_equal: missing %s", ErrProbe, side)
		}

		name := str(s, "component", "")

		comp, ok := c.Components[name]
		if !ok {
			return Result{}, fmt.Errorf("%w: files_equal: unknown %s component %q", ErrProbe, side, name)
		}

		sha, err := c.Source.CommitAt(comp.Repo, c.AsOf)
		if err != nil {
			return Result{}, fmt.Errorf("commit of %s: %w", comp.Repo, err)
		}

		path := str(s, "path", "")

		text, found, err := c.Source.File(comp.Repo, path, sha)
		if err != nil {
			return Result{}, fmt.Errorf("read %s: %w", path, err)
		}

		if !found {
			return Result{}, fmt.Errorf("%w: %s@%s: %s not found", ErrProbe, comp.Repo, short(sha), path)
		}

		texts[i] = text
	}

	if texts[0] == texts[1] {
		return Result{Done: true, Evidence: "identical"}, nil
	}

	return Result{Done: false, Evidence: "contents differ"}, nil
}

func fileLacks(spec map[string]any, c *Context) (Result, error) {
	repo, path, needle := c.Target.Repo, str(spec, "path", ""), str(spec, "text", "")
	if needle == "" {
		return Result{}, fmt.Errorf("%w: file_lacks: empty text", ErrProbe)
	}

	sha, err := c.Source.CommitAt(repo, c.AsOf)
	if err != nil {
		return Result{}, fmt.Errorf("commit of %s: %w", repo, err)
	}

	text, found, err := c.Source.File(repo, path, sha)
	if err != nil {
		return Result{}, fmt.Errorf("read %s: %w", path, err)
	}

	if !found {
		return Result{}, fmt.Errorf("%w: %s@%s: %s not found", ErrProbe, repo, short(sha), path)
	}

	var lines []string

	for i, l := range strings.Split(text, "\n") {
		if strings.Contains(l, needle) {
			lines = append(lines, strconv.Itoa(i+1))
		}
	}

	if len(lines) > 0 {
		evidence := fmt.Sprintf("%s line %s contains %q", path, strings.Join(lines, ","), needle)

		return Result{Done: false, Evidence: evidence}, nil
	}

	return Result{Done: true, Evidence: fmt.Sprintf("%s doesn't contain %q", path, needle)}, nil
}
