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

// Package github implements baton's source and issue APIs on GitHub.
// It uses release-sdk where it has the call (and its retries), and
// go-github directly for contents, labels, and sub-issues.
package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	gogithub "github.com/google/go-github/v88/github"

	rsgithub "sigs.k8s.io/release-sdk/github"

	"github.com/uwu-tools/baton/pkg/source"
	"github.com/uwu-tools/baton/pkg/writer"
)

const perPage = 100

// Client reads components and writes issues.
type Client struct {
	sdk  rsgithub.Client
	gh   *gogithub.Client
	http *http.Client

	mu       sync.Mutex
	releases map[string][]source.Release
	commits  map[string]string
	history  map[string][]source.Commit
	files    map[string]file
}

var (
	_                source.Source = (*Client)(nil)
	_                writer.Issues = (*Client)(nil)
	errNoToken                     = errors.New("no GitHub token")
	errBadRepo                     = errors.New("repo must be owner/name")
	errNotJSONObject               = errors.New("not a JSON object")
)

// Token returns GITHUB_TOKEN, else GH_TOKEN, else `gh auth token`.
func Token() (string, error) {
	for _, k := range []string{rsgithub.TokenEnvKey, "GH_TOKEN"} {
		if t := os.Getenv(k); t != "" {
			return t, nil
		}
	}

	out, err := exec.CommandContext(context.Background(), "gh", "auth", "token").Output()
	if err != nil {
		return "", fmt.Errorf("%w: set GITHUB_TOKEN or GH_TOKEN, or log in with gh: %w", errNoToken, err)
	}

	return strings.TrimSpace(string(out)), nil
}

// New returns a Client authenticated with token.
func New(token string) (*Client, error) {
	sdk, err := rsgithub.NewWithToken(token)
	if err != nil {
		return nil, fmt.Errorf("release-sdk client: %w", err)
	}

	gh, err := gogithub.NewClient(gogithub.WithAuthToken(token), gogithub.WithTimeout(30*time.Second))
	if err != nil {
		return nil, fmt.Errorf("go-github client: %w", err)
	}

	return &Client{
		sdk:      sdk.Client(),
		gh:       gh,
		http:     &http.Client{Timeout: 30 * time.Second},
		releases: map[string][]source.Release{},
		commits:  map[string]string{},
		history:  map[string][]source.Commit{},
		files:    map[string]file{},
	}, nil
}

// pages calls each for every item that list returns, following
// pagination to the last page.
func pages[T any](
	list func(*gogithub.ListOptions) ([]T, *gogithub.Response, error),
	each func(T) error,
) error {
	opts := &gogithub.ListOptions{PerPage: perPage}
	for {
		page, resp, err := list(opts)
		if err != nil {
			return err
		}

		for _, item := range page {
			if err := each(item); err != nil {
				return err
			}
		}

		if resp == nil || resp.NextPage == 0 {
			return nil
		}

		opts.Page = resp.NextPage
	}
}

func split(repo string) (owner, name string, err error) {
	owner, name, ok := strings.Cut(repo, "/")
	if !ok || owner == "" || name == "" {
		return "", "", fmt.Errorf("%w: %q", errBadRepo, repo)
	}

	return owner, name, nil
}

func isNotFound(err error) bool {
	var er *gogithub.ErrorResponse

	return errors.As(err, &er) && er.Response != nil && er.Response.StatusCode == http.StatusNotFound
}

type file struct {
	content string
	found   bool
}

// Releases implements source.Source.
func (c *Client) Releases(repo, kind string) ([]source.Release, error) {
	key := kind + ":" + repo

	c.mu.Lock()
	cached, ok := c.releases[key]
	c.mu.Unlock()

	if ok {
		return cached, nil
	}

	var (
		rels []source.Release
		err  error
	)

	if kind == "tags" {
		rels, err = c.tagReleases(repo)
	} else {
		rels, err = c.githubReleases(repo)
	}

	if err != nil {
		return nil, err
	}

	c.mu.Lock()
	c.releases[key] = rels
	c.mu.Unlock()

	return rels, nil
}

// CommitAt implements source.Source.
func (c *Client) CommitAt(repo string, asOf time.Time) (string, error) {
	key := repo + ":" + asOf.UTC().Format(time.RFC3339)

	c.mu.Lock()
	cached, ok := c.commits[key]
	c.mu.Unlock()

	if ok {
		return cached, nil
	}

	sha, err := c.fetchCommit(repo, asOf)
	if err != nil {
		return "", err
	}

	c.mu.Lock()
	c.commits[key] = sha
	c.mu.Unlock()

	return sha, nil
}

// TagCommit implements source.Source. The commits endpoint peels
// annotated tags, and the refs/tags/ prefix keeps a branch of the same
// name from matching.
func (c *Client) TagCommit(repo, tag string) (string, error) {
	owner, name, err := split(repo)
	if err != nil {
		return "", err
	}

	sha, _, err := c.gh.Repositories.GetCommitSHA1(context.Background(), owner, name, "refs/tags/"+tag, "")
	if err != nil {
		return "", fmt.Errorf("resolve %s tag %s: %w", repo, tag, err)
	}

	return sha, nil
}

// PathHistory implements source.Source.
func (c *Client) PathHistory(repo, path string, until time.Time) ([]source.Commit, error) {
	key := repo + ":" + path + ":" + until.UTC().Format(time.RFC3339)

	c.mu.Lock()
	cached, ok := c.history[key]
	c.mu.Unlock()

	if ok {
		return cached, nil
	}

	owner, name, err := split(repo)
	if err != nil {
		return nil, err
	}

	page, _, err := c.sdk.ListCommits(context.Background(), owner, name, &gogithub.CommitsListOptions{
		Path: path, Until: until, PerPage: source.MaxHistory,
	})
	if err != nil {
		return nil, fmt.Errorf("list %s commits of %s: %w", path, repo, err)
	}

	out := make([]source.Commit, 0, len(page))
	for _, rc := range page {
		out = append(out, source.Commit{SHA: rc.GetSHA(), Date: rc.GetCommit().GetCommitter().GetDate().Time})
	}

	c.mu.Lock()
	c.history[key] = out
	c.mu.Unlock()

	return out, nil
}

// FlaggedLatest implements source.Source.
func (c *Client) FlaggedLatest(repo string) (source.Release, error) {
	owner, name, err := split(repo)
	if err != nil {
		return source.Release{}, err
	}

	r, _, err := c.gh.Repositories.GetLatestRelease(context.Background(), owner, name)
	if isNotFound(err) {
		return source.Release{}, fmt.Errorf("%w: %s has no latest release", source.ErrNotFound, repo)
	}

	if err != nil {
		return source.Release{}, fmt.Errorf("latest release of %s: %w", repo, err)
	}

	return source.Release{Tag: r.GetTagName(), Published: r.GetPublishedAt().Time, Prerelease: r.GetPrerelease()}, nil
}

// File implements source.Source.
func (c *Client) File(repo, path, ref string) (content string, found bool, err error) {
	key := repo + ":" + path + "@" + ref

	c.mu.Lock()
	cached, ok := c.files[key]
	c.mu.Unlock()

	if ok {
		return cached.content, cached.found, nil
	}

	f, err := c.fetchFile(repo, path, ref)
	if err != nil {
		return "", false, err
	}

	c.mu.Lock()
	c.files[key] = f
	c.mu.Unlock()

	return f.content, f.found, nil
}

// GetJSON implements source.Source.
func (c *Client) GetJSON(url string) (map[string]any, error) {
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, url, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("GET %s: %w", url, err)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GET %s: %w", url, err)
	}

	defer func() { _ = resp.Body.Close() }() // read errors are checked; close after read can't lose data

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: %w: %s", url, source.ErrHTTP, resp.Status)
	}

	var m map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&m); err != nil {
		return nil, fmt.Errorf("GET %s: %w: %w", url, errNotJSONObject, err)
	}

	return m, nil
}

// LabelExists implements writer.Issues.
func (c *Client) LabelExists(repo, label string) (bool, error) {
	owner, name, err := split(repo)
	if err != nil {
		return false, err
	}

	_, _, err = c.gh.Issues.GetLabel(context.Background(), owner, name, label)
	if isNotFound(err) {
		return false, nil
	}

	if err != nil {
		return false, fmt.Errorf("get label %s in %s: %w", label, repo, err)
	}

	return true, nil
}

// CreateLabel implements writer.Issues.
func (c *Client) CreateLabel(repo, label, color, description string) error {
	owner, name, err := split(repo)
	if err != nil {
		return err
	}

	_, _, err = c.gh.Issues.CreateLabel(context.Background(), owner, name, &gogithub.Label{
		Name: &label, Color: &color, Description: &description,
	})
	if err != nil {
		return fmt.Errorf("create label %s in %s: %w", label, repo, err)
	}

	return nil
}

func toIssue(i *gogithub.Issue) writer.Issue {
	return writer.Issue{
		ID: i.GetID(), Number: i.GetNumber(), State: i.GetState(), Title: i.GetTitle(), Body: i.GetBody(),
	}
}

// ListIssues implements writer.Issues.
func (c *Client) ListIssues(repo, label string) ([]writer.Issue, error) {
	owner, name, err := split(repo)
	if err != nil {
		return nil, err
	}

	var out []writer.Issue

	opts := &gogithub.IssueListByRepoOptions{
		State: "all", Labels: []string{label}, ListOptions: gogithub.ListOptions{PerPage: perPage},
	}
	for {
		page, resp, err := c.sdk.ListIssues(context.Background(), owner, name, opts)
		if err != nil {
			return nil, fmt.Errorf("list issues in %s: %w", repo, err)
		}

		for _, i := range page {
			if i.IsPullRequest() {
				continue
			}

			out = append(out, toIssue(i))
		}

		if resp == nil || resp.NextPage == 0 {
			return out, nil
		}

		opts.ListOptions.Page = resp.NextPage
	}
}

// CreateIssue implements writer.Issues.
func (c *Client) CreateIssue(repo, title, body string, labels, assignees []string) (writer.Issue, error) {
	owner, name, err := split(repo)
	if err != nil {
		return writer.Issue{}, err
	}

	req := &gogithub.IssueRequest{Title: &title, Body: &body, Labels: &labels}
	if len(assignees) > 0 {
		req.Assignees = &assignees
	}

	i, err := c.sdk.CreateIssue(context.Background(), owner, name, req)
	if err != nil {
		return writer.Issue{}, fmt.Errorf("create issue in %s: %w", repo, err)
	}

	return toIssue(i), nil
}

// UpdateIssue implements writer.Issues.
func (c *Client) UpdateIssue(repo string, number int, u writer.Update) (writer.Issue, error) {
	owner, name, err := split(repo)
	if err != nil {
		return writer.Issue{}, err
	}

	req := &gogithub.IssueRequest{}
	set := func(dst **string, v string) {
		if v != "" {
			*dst = &v
		}
	}
	set(&req.State, u.State)
	set(&req.StateReason, u.StateReason)
	set(&req.Title, u.Title)
	set(&req.Body, u.Body)

	i, _, err := c.sdk.UpdateIssue(context.Background(), owner, name, number, req)
	if err != nil {
		return writer.Issue{}, fmt.Errorf("update %s#%d: %w", repo, number, err)
	}

	return toIssue(i), nil
}

// Comment implements writer.Issues.
func (c *Client) Comment(repo string, number int, body string) error {
	owner, name, err := split(repo)
	if err != nil {
		return err
	}

	if _, _, err := c.sdk.CreateComment(context.Background(), owner, name, number, body); err != nil {
		return fmt.Errorf("comment on %s#%d: %w", repo, number, err)
	}

	return nil
}

// SubIssueIDs implements writer.Issues.
func (c *Client) SubIssueIDs(repo string, number int) ([]int64, error) {
	owner, name, err := split(repo)
	if err != nil {
		return nil, err
	}

	var ids []int64

	err = pages(func(opts *gogithub.ListOptions) ([]*gogithub.SubIssue, *gogithub.Response, error) {
		return c.gh.SubIssue.ListByIssue(context.Background(), owner, name, int64(number), opts)
	}, func(s *gogithub.SubIssue) error {
		ids = append(ids, (*gogithub.Issue)(s).GetID())

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("list sub-issues of %s#%d: %w", repo, number, err)
	}

	return ids, nil
}

// AddSubIssue implements writer.Issues.
func (c *Client) AddSubIssue(repo string, number int, childID int64) error {
	owner, name, err := split(repo)
	if err != nil {
		return err
	}

	_, _, err = c.gh.SubIssue.Add(context.Background(), owner, name, int64(number),
		gogithub.SubIssueRequest{SubIssueID: childID})
	if err != nil {
		return fmt.Errorf("add sub-issue to %s#%d: %w", repo, number, err)
	}

	return nil
}

func (c *Client) githubReleases(repo string) ([]source.Release, error) {
	owner, name, err := split(repo)
	if err != nil {
		return nil, err
	}

	var out []source.Release

	err = pages(func(opts *gogithub.ListOptions) ([]*gogithub.RepositoryRelease, *gogithub.Response, error) {
		return c.sdk.ListReleases(context.Background(), owner, name, opts)
	}, func(r *gogithub.RepositoryRelease) error {
		if r.GetDraft() {
			return nil // not public
		}

		out = append(out, source.Release{
			Tag: r.GetTagName(), Published: r.GetPublishedAt().Time, Prerelease: r.GetPrerelease(),
		})

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("list releases of %s: %w", repo, err)
	}

	return out, nil
}

// tagReleases dates each version tag by its commit's committer date,
// since tags have no publish time of their own.
func (c *Client) tagReleases(repo string) ([]source.Release, error) {
	owner, name, err := split(repo)
	if err != nil {
		return nil, err
	}

	var out []source.Release

	err = pages(func(opts *gogithub.ListOptions) ([]*gogithub.RepositoryTag, *gogithub.Response, error) {
		return c.sdk.ListTags(context.Background(), owner, name, opts)
	}, func(t *gogithub.RepositoryTag) error {
		commit, _, err := c.sdk.GetRepoCommit(context.Background(), owner, name, t.GetCommit().GetSHA())
		if err != nil {
			return fmt.Errorf("%s tag %s: %w", repo, t.GetName(), err)
		}

		out = append(out, source.Release{
			Tag: t.GetName(), Published: commit.GetCommit().GetCommitter().GetDate().Time,
			Commit: t.GetCommit().GetSHA(),
		})

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("list tags of %s: %w", repo, err)
	}

	return out, nil
}

func (c *Client) fetchCommit(repo string, asOf time.Time) (string, error) {
	owner, name, err := split(repo)
	if err != nil {
		return "", err
	}

	commits, _, err := c.sdk.ListCommits(context.Background(), owner, name, &gogithub.CommitsListOptions{
		Until: asOf, PerPage: 1,
	})
	if err != nil {
		return "", fmt.Errorf("list commits of %s: %w", repo, err)
	}

	if len(commits) == 0 {
		return "", fmt.Errorf("%w: %s has no default-branch commit before %s",
			source.ErrNotFound, repo, asOf.Format(time.RFC3339))
	}

	return commits[0].GetSHA(), nil
}

func (c *Client) fetchFile(repo, path, ref string) (file, error) {
	owner, name, err := split(repo)
	if err != nil {
		return file{}, err
	}

	fc, _, _, err := c.gh.Repositories.GetContents(context.Background(), owner, name, path,
		&gogithub.RepositoryContentGetOptions{Ref: ref})
	if isNotFound(err) {
		return file{}, nil
	}

	if err != nil {
		return file{}, fmt.Errorf("read %s/%s@%s: %w", repo, path, ref, err)
	}

	if fc == nil {
		return file{}, fmt.Errorf("read %s/%s@%s: %w", repo, path, ref, source.ErrNotFile)
	}

	text, err := fc.GetContent()
	if err != nil {
		return file{}, fmt.Errorf("decode %s/%s@%s: %w", repo, path, ref, err)
	}

	return file{text, true}, nil
}
