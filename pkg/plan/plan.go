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

// Package plan maps a reconcile report to issue changes.
package plan

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/uwu-tools/baton/pkg/reconcile"
)

// Label marks every issue baton manages.
const Label = "baton"

// Op is an issue change.
type Op string

// Issue changes.
const (
	// OpenOrUpdate creates the issue, or reopens and updates it.
	OpenOrUpdate Op = "open-or-update"
	// UpdateIfOpen keeps an open issue current but never opens one.
	UpdateIfOpen Op = "update-if-open"
	// CloseIfOpen closes the issue as completed.
	CloseIfOpen Op = "close-if-open"
)

// Item is an issue change a writer would make. Issues carry a hidden
// marker with Key, so a writer finds them again on every run.
type Item struct {
	Op         Op       `json:"op"`
	Repo       string   `json:"repo"`
	Key        string   `json:"key"`
	IsParent   bool     `json:"is_parent,omitempty"`
	Parent     string   `json:"parent,omitempty"`      // parent issue key
	ParentRepo string   `json:"parent_repo,omitempty"` // repo holding the parent
	Title      string   `json:"title,omitempty"`
	Body       string   `json:"body,omitempty"`
	Assignees  []string `json:"assignees,omitempty"`
	Evidence   string   `json:"evidence,omitempty"`
}

var markerRE = regexp.MustCompile(`<!-- baton key=(\S+)`)

// KeyFromBody returns the obligation key in an issue's hidden marker.
func KeyFromBody(body string) string {
	if m := markerRE.FindStringSubmatch(body); m != nil {
		return m[1]
	}

	return ""
}

const (
	checked   = "[x]"
	unchecked = "[ ]"
)

var marks = map[reconcile.State]string{
	reconcile.Done: checked, reconcile.Waived: checked, reconcile.NotApplicable: checked,
	reconcile.Pending: unchecked, reconcile.Overdue: unchecked, reconcile.Blocked: unchecked,
	reconcile.Unknown: unchecked, reconcile.Error: unchecked,
}

// CodeSpan renders text read from repos, releases, or endpoints as
// inline code, so it can't mention people, link issues, or inject
// Markdown. The fence is longer than any run of backticks inside.
func CodeSpan(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if s == "" {
		return ""
	}

	longest, run := 0, 0

	for _, r := range s {
		if r == '`' {
			run++
			longest = max(longest, run)
		} else {
			run = 0
		}
	}

	fence := strings.Repeat("`", longest+1)
	if strings.HasPrefix(s, "`") || strings.HasSuffix(s, "`") {
		return fence + " " + s + " " + fence
	}

	return fence + s + fence
}

// rulesAttr renders the rules digest for an issue marker.
func rulesAttr(rules string) string {
	if rules == "" {
		return ""
	}

	return " rules=" + rules
}

// parentBody renders the rollout checklist shown in the parent issue.
func parentBody(in *reconcile.Instance, rules string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "<!-- baton key=%s%s -->\n", in.Key, rulesAttr(rules))
	fmt.Fprintf(&b, "Follow-ups owed for %s.\n\n", CodeSpan(in.Binding))

	for i := range in.Results {
		r := &in.Results[i]
		fmt.Fprintf(&b, "- %s **%s** `%s` %s: %s\n", marks[r.State], r.State, r.Target, r.Action, CodeSpan(r.Evidence))
	}

	return b.String()
}

func childBody(r *reconcile.Result, parentKey, rules string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "<!-- baton key=%s parent=%s%s -->\n", r.Key(), parentKey, rulesAttr(rules))
	fmt.Fprintf(&b, "State: **%s**\n\nEvidence: %s\n\n", r.State, CodeSpan(r.Evidence))
	b.WriteString("This issue closes itself when the condition holds. ")
	b.WriteString("Closing it by hand doesn't settle the obligation.\n")

	return b.String()
}

// Plan maps a report to issue changes. Blocked, unknown, errored, and
// not-applicable obligations never get a new child issue; a blocked one
// updates its issue if one is already open.
func Plan(rep *reconcile.Report) []Item {
	var out []Item

	child := func(r *reconcile.Result, parent, parentRepo string) {
		it := Item{Repo: r.Target, Key: r.Key(), Parent: parent, ParentRepo: parentRepo}
		switch r.State {
		case reconcile.Pending, reconcile.Overdue:
			it.Op = OpenOrUpdate
		case reconcile.Blocked:
			it.Op = UpdateIfOpen
		case reconcile.Done, reconcile.Waived:
			it.Op = CloseIfOpen
			it.Evidence = fmt.Sprintf("%s: %s", r.State, CodeSpan(r.Evidence))
			out = append(out, it)

			return
		case reconcile.Unknown, reconcile.NotApplicable, reconcile.Error:
			return
		}

		it.Title, it.Body, it.Assignees = r.Action, childBody(r, parent, rep.Rules), r.Owners
		out = append(out, it)
	}

	for i := range rep.Instances {
		in := &rep.Instances[i]
		if in.Key == "" {
			continue // no trigger yet
		}

		parent := Item{Repo: in.Home, Key: in.Key, IsParent: true}
		if in.Complete {
			parent.Op, parent.Evidence = CloseIfOpen, "rollout complete"
		} else {
			parent.Op = OpenOrUpdate
			parent.Title = "[rollout] " + strings.Replace(in.Binding, "@", " ", 1)
			parent.Body = parentBody(in, rep.Rules)
		}

		out = append(out, parent)

		for j := range in.Results {
			child(&in.Results[j], in.Key, in.Home)
		}
	}

	for i := range rep.Invariants {
		child(&rep.Invariants[i], "", "")
	}

	return out
}
