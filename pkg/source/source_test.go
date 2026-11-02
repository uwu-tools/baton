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

package source_test

import (
	"regexp"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/uwu-tools/baton/pkg/source"
	"github.com/uwu-tools/baton/pkg/source/sourcefakes"
)

func day(s string) time.Time {
	d, err := time.Parse(time.DateOnly, s)
	if err != nil {
		panic(err)
	}

	return d
}

var now = day("2026-06-01")

func fake() *sourcefakes.FakeSource {
	f := &sourcefakes.FakeSource{}
	f.ReleasesReturns([]source.Release{
		{Tag: "v1.0.0", Published: day("2026-01-01")},
		{Tag: "v1.1.0", Published: day("2026-02-01")},
		{Tag: "v1.1.1", Published: day("2026-03-01")},
		{Tag: "v2.0.0-rc.1", Published: day("2026-03-15")},
		{Tag: "v1.2.0", Published: day("2026-04-01"), Prerelease: true},
		{Tag: "nightly-2026-05-01", Published: day("2026-05-01")},
		{Tag: "v2.0.0", Published: day("2026-07-01")}, // after now
	}, nil)
	f.FlaggedLatestReturns(source.Release{Tag: "v1.1.0"}, nil)

	return f
}

func tags(t *testing.T, f *source.Filter, asOf time.Time) []string {
	t.Helper()

	rels, err := f.Releases(fake(), "ex/core", asOf, now)
	require.NoError(t, err)

	out := make([]string, 0, len(rels))
	for _, r := range rels {
		out = append(out, r.Tag)
	}

	return out
}

func TestFilter(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		filter source.Filter
		want   []string
	}{
		"default: stable versions, highest first": {
			source.Filter{}, []string{"v1.1.1", "v1.1.0", "v1.0.0"},
		},
		"prereleases by suffix and by forge flag": {
			source.Filter{Prerelease: true}, []string{"v2.0.0-rc.1", "v1.2.0", "v1.1.1", "v1.1.0", "v1.0.0"},
		},
		"published order takes any tag": {
			source.Filter{Order: source.OrderPublished},
			[]string{"nightly-2026-05-01", "v1.1.1", "v1.1.0", "v1.0.0"},
		},
		"tag patterns must all match": {
			source.Filter{
				Order: source.OrderPublished,
				Tags:  []*regexp.Regexp{regexp.MustCompile(`^v1\.`), regexp.MustCompile(`\.0$`)},
			},
			[]string{"v1.1.0", "v1.0.0"},
		},
		"level minor skips patch releases": {
			source.Filter{Level: source.LevelMinor}, []string{"v1.1.0", "v1.0.0"},
		},
		"level major skips minor releases": {
			source.Filter{Level: source.LevelMajor}, []string{"v1.0.0"},
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.want, tags(t, &tc.filter, now))
		})
	}
}

func TestFilterForge(t *testing.T) {
	t.Parallel()

	f := source.Filter{Order: source.OrderForge}
	require.Equal(t, []string{"v1.1.0", "nightly-2026-05-01", "v1.1.1", "v1.0.0"}, tags(t, &f, now),
		"flagged first, then any tag by date")

	_, err := f.Releases(fake(), "ex/core", day("2026-03-01"), now)
	require.ErrorIs(t, err, source.ErrUnknowable, "the flag can't be read for a past date")

	src := fake()
	src.FlaggedLatestReturns(source.Release{Tag: "nightly-2026-05-01"}, nil)
	_, err = (&source.Filter{Order: source.OrderForge, Tags: []*regexp.Regexp{regexp.MustCompile(`^v`)}}).
		Releases(src, "ex/core", now, now)
	require.ErrorIs(t, err, source.ErrNotFound, "a flagged release the filter rejects is an error")

	src = fake()
	src.FlaggedLatestReturns(source.Release{}, source.ErrNotFound)
	rels, err := f.Releases(src, "ex/core", now, now)
	require.NoError(t, err)
	require.Empty(t, rels, "no flagged release means no release yet")
}
