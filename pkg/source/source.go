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

// Package source defines what baton reads about components.
package source

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"

	"golang.org/x/mod/semver"
)

//go:generate go tool counterfeiter -generate
//counterfeiter:generate . Source

// Errors a Source returns for answers that aren't API failures.
var (
	ErrNotFound = errors.New("not found")
	ErrNotFile  = errors.New("path is not a file")
	ErrHTTP     = errors.New("unexpected HTTP status")

	// ErrUnknowable means a question can't be answered for a past date
	// by design, such as the forge's current latest flag in a replay.
	ErrUnknowable = errors.New("can't be evaluated for a past date")
)

// Release is a version of a component and when it was published.
// Commit is the commit the tag points to, when the listing provides it;
// otherwise TagCommit resolves it.
type Release struct {
	Tag        string
	Published  time.Time
	Commit     string
	Prerelease bool // the forge's prerelease flag; tags have none
}

// Commit is a commit and its committer date.
type Commit struct {
	SHA  string
	Date time.Time
}

// MaxHistory caps how many commits PathHistory returns.
const MaxHistory = 100

// Source is everything the probes read. The GitHub implementation is
// in internal/github; tests use the generated fake.
type Source interface {
	// Releases returns stable releases. kind is "releases" (GitHub
	// releases) or "tags" (version tags, dated by their commit).
	Releases(repo, kind string) ([]Release, error)
	// TagCommit returns the commit a tag points to, peeling annotated
	// tags.
	TagCommit(repo, tag string) (string, error)
	// CommitAt returns the default-branch commit as of a time.
	CommitAt(repo string, asOf time.Time) (string, error)
	// PathHistory returns up to MaxHistory default-branch commits that
	// touched path, committed at or before until, newest first.
	PathHistory(repo, path string, until time.Time) ([]Commit, error)
	// FlaggedLatest returns the release the forge marks as latest.
	FlaggedLatest(repo string) (Release, error)
	// File returns a file's content at ref; found is false on 404.
	File(repo, path, ref string) (content string, found bool, err error)
	// GetJSON fetches a JSON object from a URL.
	GetJSON(url string) (map[string]any, error)
}

// Where a component's releases come from.
const (
	KindReleases = "releases"
	KindTags     = "tags"
)

// Release orders.
const (
	OrderVersion   = "version"
	OrderPublished = "published"
	OrderForge     = "forge"
)

// Trigger levels: the smallest SemVer change that owes work.
const (
	LevelMajor = "major"
	LevelMinor = "minor"
	LevelPatch = "patch"
)

// liveWindow is how far before now an as-of time still counts as now.
const liveWindow = time.Hour

// Filter says which of a component's releases count, and in what order.
type Filter struct {
	Kind       string           // "releases" (default) or "tags"
	Tags       []*regexp.Regexp // every pattern must match
	Prerelease bool             // count prereleases
	Order      string           // OrderVersion (default), OrderPublished, or OrderForge
	Level      string           // triggers only; "" or LevelPatch counts every release
}

// Releases returns the counted releases published by asOf, latest
// first. For OrderForge the forge's flagged release comes first, then
// the rest by publish date; a past asOf returns ErrUnknowable.
func (f *Filter) Releases(src Source, repo string, asOf, now time.Time) ([]Release, error) {
	kind := f.Kind
	if kind == "" {
		kind = KindReleases
	}

	all, err := src.Releases(repo, kind)
	if err != nil {
		return nil, fmt.Errorf("releases of %s: %w", repo, err)
	}

	out := make([]Release, 0, len(all))
	for i := range all {
		if !all[i].Published.After(asOf) && f.counts(&all[i]) {
			out = append(out, all[i])
		}
	}

	switch f.Order {
	case OrderPublished, OrderForge:
		slices.SortStableFunc(out, func(a, b Release) int { return b.Published.Compare(a.Published) })
	default:
		slices.SortStableFunc(out, func(a, b Release) int { return semver.Compare(b.Tag, a.Tag) })
	}

	if f.Order != OrderForge {
		return out, nil
	}

	if asOf.Before(now.Add(-liveWindow)) {
		return nil, fmt.Errorf("%s latest flag: %w", repo, ErrUnknowable)
	}

	latest, err := src.FlaggedLatest(repo)
	if errors.Is(err, ErrNotFound) {
		return nil, nil
	}

	if err != nil {
		return nil, fmt.Errorf("latest release of %s: %w", repo, err)
	}

	for i := range out {
		if out[i].Tag == latest.Tag {
			flagged := out[i]

			return append([]Release{flagged}, slices.Delete(out, i, i+1)...), nil
		}
	}

	return nil, fmt.Errorf("%w: %s marks %s as latest, but it doesn't count as a release", ErrNotFound, repo, latest.Tag)
}

// Latest returns the latest counted release, or nil if there is none.
func (f *Filter) Latest(src Source, repo string, asOf, now time.Time) (*Release, error) {
	rels, err := f.Releases(src, repo, asOf, now)
	if err != nil || len(rels) == 0 {
		return nil, err
	}

	return &rels[0], nil
}

// counts applies the filter to one release.
func (f *Filter) counts(r *Release) bool {
	for _, re := range f.Tags {
		if !re.MatchString(r.Tag) {
			return false
		}
	}

	isVersion := semver.IsValid(r.Tag) && semver.Build(r.Tag) == ""
	pre := r.Prerelease || (isVersion && semver.Prerelease(r.Tag) != "")

	if pre && !f.Prerelease {
		return false
	}

	if f.Order == "" || f.Order == OrderVersion {
		if !isVersion {
			return false
		}
	}

	return atLevel(r.Tag, f.Level)
}

// atLevel reports whether a version is at least the given change level:
// for LevelMinor, vX.Y.0; for LevelMajor, vX.0.0.
func atLevel(tag, level string) bool {
	if level == "" || level == LevelPatch {
		return true
	}

	parts := strings.SplitN(strings.TrimPrefix(semver.Canonical(tag), "v"), ".", 3)
	if len(parts) < 3 {
		return false
	}

	patch, _, _ := strings.Cut(parts[2], "-")
	if level == LevelMinor {
		return patch == "0"
	}

	return parts[1] == "0" && patch == "0"
}
