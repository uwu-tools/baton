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

// Package writer carries out an issue plan on GitHub.
package writer

import (
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/uwu-tools/baton/pkg/plan"
	"github.com/uwu-tools/baton/pkg/reconcile"
)

//go:generate go tool counterfeiter -generate
//counterfeiter:generate . Issues

// Issue is the part of a GitHub issue the writer uses.
type Issue struct {
	ID     int64
	Number int
	State  string // "open" or "closed"
	Title  string
	Body   string
}

// Update changes an issue. Empty fields are left alone.
type Update struct {
	State       string
	StateReason string
	Title       string
	Body        string
}

// Issues is the issue API the writer needs. Repos are "owner/name".
type Issues interface {
	LabelExists(repo, label string) (bool, error)
	CreateLabel(repo, label, color, description string) error
	// ListIssues returns open and closed issues with label, without PRs.
	ListIssues(repo, label string) ([]Issue, error)
	CreateIssue(repo, title, body string, labels, assignees []string) (Issue, error)
	UpdateIssue(repo string, number int, u Update) (Issue, error)
	Comment(repo string, number int, body string) error
	SubIssueIDs(repo string, number int) ([]int64, error)
	AddSubIssue(repo string, number int, childID int64) error
}

// Options control where and how issues are written.
type Options struct {
	// RedirectOwner sends every issue for owner/name to
	// RedirectOwner/name, prefixes titles with the intended repo, and
	// drops assignees so no real maintainer is notified.
	RedirectOwner string
	// CreateLabel creates the baton label when a repo lacks it.
	// Otherwise a missing label is an error.
	CreateLabel bool
}

// ErrNoRedirect is returned when writing without a sandbox owner. The
// preview never writes to the real component repos.
var ErrNoRedirect = errors.New("apply needs --redirect-owner: the preview doesn't write to real repos")

// ErrUnknownOp is returned for a plan item the writer can't carry out.
var ErrUnknownOp = errors.New("unknown plan op")

// Issue states.
const (
	stateOpen   = "open"
	stateClosed = "closed"
)

// ErrMissingLabel is returned when a repo lacks the baton label.
var ErrMissingLabel = errors.New("missing label")

// Validate checks the options.
func (o *Options) Validate() error {
	if o.RedirectOwner == "" {
		return ErrNoRedirect
	}

	return nil
}

// Writer applies plans.
type Writer struct {
	opts   *Options
	api    Issues
	log    []string
	issues map[string]map[string]*Issue // repo -> key -> issue
}

// New returns a Writer for opts over api.
func New(opts *Options, api Issues) *Writer {
	return &Writer{opts: opts, api: api, issues: map[string]map[string]*Issue{}}
}

// Run carries out items. Issues whose key the report doesn't know (an
// older release, a removed rule) are closed as superseded. Issues for
// blocked, unknown, or errored obligations are left open. It stops at
// the first API error and returns what it did so far.
func (w *Writer) Run(rep *reconcile.Report, items []plan.Item) ([]string, error) {
	if err := w.opts.Validate(); err != nil {
		return nil, err
	}

	known := map[string]bool{}
	for i := range rep.Instances {
		known[rep.Instances[i].Key] = true
	}

	all := rep.All()
	for i := range all {
		known[all[i].Key()] = true
	}

	parents := map[string]*Issue{} // parent key -> issue

	for i := range items {
		it := &items[i]
		if !it.IsParent {
			continue
		}

		is, found, err := w.do(it)
		if err != nil {
			return w.log, err
		}

		if found {
			parents[it.Key] = is
		}
	}

	for i := range items {
		it := &items[i]
		if it.IsParent {
			continue
		}

		is, found, err := w.do(it)
		if err != nil {
			return w.log, err
		}

		parent, ok := parents[it.Parent]
		if it.Op != plan.OpenOrUpdate || !ok || !found {
			continue
		}

		if err := w.link(w.repo(it.ParentRepo), parent, is); err != nil {
			return w.log, err
		}
	}

	return w.log, w.supersede(known)
}

func (w *Writer) logf(format string, a ...any) {
	w.log = append(w.log, fmt.Sprintf(format, a...))
}

func (w *Writer) repo(r string) string {
	if w.opts.RedirectOwner == "" {
		return r
	}

	_, name, _ := strings.Cut(r, "/")

	return w.opts.RedirectOwner + "/" + name
}

func (w *Writer) supersede(known map[string]bool) error {
	repos := make([]string, 0, len(w.issues))
	for repo := range w.issues {
		repos = append(repos, repo)
	}

	sort.Strings(repos)

	for _, repo := range repos {
		keys := make([]string, 0, len(w.issues[repo]))
		for key := range w.issues[repo] {
			keys = append(keys, key)
		}

		sort.Strings(keys)

		for _, key := range keys {
			is := w.issues[repo][key]
			if is.State == stateOpen && !known[key] {
				if err := w.close(repo, is, "not_planned",
					"Superseded: baton no longer tracks this obligation "+
						"(a newer trigger replaced it, or the rule was removed)."); err != nil {
					return err
				}
			}
		}
	}

	return nil
}

// load ensures the label exists and indexes baton issues in a repo.
func (w *Writer) load(repo string) (map[string]*Issue, error) {
	if m, ok := w.issues[repo]; ok {
		return m, nil
	}

	ok, err := w.api.LabelExists(repo, plan.Label)
	if err != nil {
		return nil, fmt.Errorf("label in %s: %w", repo, err)
	}

	if !ok {
		if !w.opts.CreateLabel {
			return nil, fmt.Errorf("%w: %s has no %q label; create it first", ErrMissingLabel, repo, plan.Label)
		}

		if err := w.api.CreateLabel(repo, plan.Label, "ededed", "Follow-up work tracked by baton"); err != nil {
			return nil, fmt.Errorf("create label in %s: %w", repo, err)
		}

		w.logf("created label %s in %s", plan.Label, repo)
	}

	all, err := w.api.ListIssues(repo, plan.Label)
	if err != nil {
		return nil, fmt.Errorf("list %s issues: %w", repo, err)
	}

	m := map[string]*Issue{}

	for i := range all {
		is := &all[i]
		if k := plan.KeyFromBody(is.Body); k != "" {
			// Keep the open one if a key appears twice.
			if old, dup := m[k]; !dup || old.State != stateOpen {
				m[k] = is
			}
		}
	}

	w.issues[repo] = m

	return m, nil
}

// do carries out one item. found reports whether an issue exists for
// the item afterwards.
func (w *Writer) do(it *plan.Item) (is *Issue, found bool, err error) {
	repo := w.repo(it.Repo)

	m, err := w.load(repo)
	if err != nil {
		return nil, false, err
	}

	is, exists := m[it.Key]
	switch it.Op {
	case plan.OpenOrUpdate:
		is, err = w.openOrUpdate(repo, it, m)

		return is, err == nil, err
	case plan.UpdateIfOpen:
		if exists && is.State == stateOpen {
			is, err = w.openOrUpdate(repo, it, m)

			return is, err == nil, err
		}
	case plan.CloseIfOpen:
		if exists {
			return is, true, w.close(repo, is, "completed", "Done: "+it.Evidence)
		}
	default:
		return nil, false, fmt.Errorf("%w: %q", ErrUnknownOp, it.Op)
	}

	return is, exists, nil
}

func (w *Writer) openOrUpdate(repo string, it *plan.Item, m map[string]*Issue) (*Issue, error) {
	title, assignees := it.Title, it.Assignees
	if w.opts.RedirectOwner != "" {
		title = "[" + it.Repo + "] " + title
		assignees = nil
	}

	is, ok := m[it.Key]
	if !ok {
		created, err := w.api.CreateIssue(repo, title, it.Body, []string{plan.Label}, assignees)
		if err != nil {
			return nil, fmt.Errorf("create issue in %s: %w", repo, err)
		}

		m[it.Key] = &created
		w.logf("opened %s#%d %s", repo, created.Number, title)

		return &created, nil
	}

	var u Update

	reopen := is.State != stateOpen
	if reopen {
		u.State = stateOpen
	}

	if is.Title != title {
		u.Title = title
	}

	if is.Body != it.Body {
		u.Body = it.Body
	}

	if u == (Update{}) {
		return is, nil
	}

	updated, err := w.api.UpdateIssue(repo, is.Number, u)
	if err != nil {
		return nil, fmt.Errorf("update %s#%d: %w", repo, is.Number, err)
	}

	*is = updated
	if reopen {
		if err := w.api.Comment(repo, is.Number, "Reopened: this obligation is still unmet."); err != nil {
			return nil, fmt.Errorf("comment on %s#%d: %w", repo, is.Number, err)
		}

		w.logf("reopened %s#%d", repo, is.Number)
	} else {
		w.logf("updated %s#%d", repo, is.Number)
	}

	return is, nil
}

func (w *Writer) close(repo string, is *Issue, reason, note string) error {
	if is.State != stateOpen {
		return nil
	}

	if err := w.api.Comment(repo, is.Number, note); err != nil {
		return fmt.Errorf("comment on %s#%d: %w", repo, is.Number, err)
	}

	updated, err := w.api.UpdateIssue(repo, is.Number, Update{State: stateClosed, StateReason: reason})
	if err != nil {
		return fmt.Errorf("close %s#%d: %w", repo, is.Number, err)
	}

	*is = updated
	w.logf("closed %s#%d (%s)", repo, is.Number, reason)

	return nil
}

// link makes child a sub-issue of parent unless it already is.
func (w *Writer) link(parentRepo string, parent, child *Issue) error {
	ids, err := w.api.SubIssueIDs(parentRepo, parent.Number)
	if err != nil {
		return fmt.Errorf("list sub-issues of %s#%d: %w", parentRepo, parent.Number, err)
	}

	if slices.Contains(ids, child.ID) {
		return nil
	}

	if err := w.api.AddSubIssue(parentRepo, parent.Number, child.ID); err != nil {
		return fmt.Errorf("link under %s#%d: %w", parentRepo, parent.Number, err)
	}

	w.logf("linked #%d under %s#%d", child.Number, parentRepo, parent.Number)

	return nil
}
