package main

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/shurcooL/githubv4"
)

func contribution(name string, occurredAt string) repoContribution {
	t, err := time.Parse(time.RFC3339, occurredAt)
	if err != nil {
		panic(err)
	}
	return repoContribution{
		ID:         "id-" + name,
		Repository: qlRepository{NameWithOwner: githubv4.String(name)},
		OccurredAt: t,
	}
}

func TestWindowsBefore(t *testing.T) {
	now := time.Date(2026, time.October, 13, 12, 0, 0, 0, time.UTC)
	windows := windowsBefore(now, 2, 6)

	if len(windows) != 2 {
		t.Fatalf("got %d windows, want 2", len(windows))
	}
	if !windows[0].To.Equal(now) {
		t.Errorf("first window ends at %s, want %s", windows[0].To, now)
	}
	if !windows[0].From.Equal(windows[1].To) {
		t.Errorf("windows are not consecutive: %s and %s", windows[0].From, windows[1].To)
	}
	want := time.Date(2025, time.October, 13, 12, 0, 0, 0, time.UTC)
	if !windows[1].From.Equal(want) {
		t.Errorf("last window starts at %s, want %s", windows[1].From, want)
	}
	for _, w := range windows {
		if !w.From.Before(w.To) {
			t.Errorf("window %s to %s is empty", w.From, w.To)
		}
	}
}

func TestMergeContributions(t *testing.T) {
	merged := mergeContributions(nil, []repoContribution{
		contribution("a/a", "2026-10-01T00:00:00Z"),
		contribution("b/b", "2026-09-01T00:00:00Z"),
	})
	merged = mergeContributions(merged, []repoContribution{
		contribution("b/b", "2026-09-15T00:00:00Z"), // newer than the known one
		contribution("a/a", "2026-01-01T00:00:00Z"), // older than the known one
		contribution("c/c", "2026-02-01T00:00:00Z"),
	})

	got := map[string]string{}
	for _, c := range merged {
		got[string(c.Repository.NameWithOwner)] = c.OccurredAt.Format("2006-01-02")
	}
	want := map[string]string{
		"a/a": "2026-10-01",
		"b/b": "2026-09-15",
		"c/c": "2026-02-01",
	}
	if len(merged) != len(want) {
		t.Fatalf("got %d repositories, want %d: %v", len(merged), len(want), got)
	}
	for name, day := range want {
		if got[name] != day {
			t.Errorf("%s: got %q, want %q", name, got[name], day)
		}
	}
}

// fakeContributions answers the contributions query with one set of
// repositories per window, most recent window first, and the releases
// query from the given releases per repository name. Windows that are not
// given have no contributions.
func fakeContributions(t *testing.T, windows []list, releases map[string]list) *[]graphQLRequest {
	t.Helper()

	window := 0
	return fakeGitHub(t, "octocat", func(req graphQLRequest) interface{} {
		switch {
		case strings.Contains(req.Query, "contributionsCollection"):
			if window >= contributionWindows {
				t.Errorf("unexpected contributions query number %d", window+1)
			}
			repos := list{}
			if window < len(windows) {
				repos = windows[window]
			}
			window++
			return obj{"user": obj{"contributionsCollection": obj{"commitContributionsByRepository": repos}}}

		case strings.Contains(req.Query, "nodes(ids: $ids)"):
			nodes := list{}
			for _, id := range req.Variables["ids"].([]interface{}) {
				name := strings.TrimPrefix(id.(string), "id-")
				nodes = append(nodes, obj{"id": id, "releases": obj{"nodes": releases[name]}})
			}
			return obj{"nodes": nodes}
		}

		t.Errorf("unexpected query: %s", req.Query)
		return obj{}
	})
}

func fakeContribution(name, occurredAt string) obj {
	return obj{
		"contributions": obj{"edges": list{obj{"cursor": "c", "node": obj{"occurredAt": occurredAt}}}},
		"repository":    with(fakeRepo(name), "id", "id-"+name),
	}
}

func fakeRelease(tag, publishedAt string) obj {
	return obj{
		"name":         "Release " + tag,
		"tagName":      tag,
		"publishedAt":  publishedAt,
		"url":          "https://example.org/releases/" + tag,
		"isPrerelease": false,
		"isDraft":      false,
	}
}

func TestRecentContributions(t *testing.T) {
	requests := fakeContributions(t, []list{
		{
			fakeContribution("octocat/octocat", "2026-10-12T07:00:00Z"), // the profile repository
			fakeContribution("org/newer", "2026-10-10T07:00:00Z"),
			with(fakeContribution("org/secret", "2026-10-09T07:00:00Z"), "repository",
				with(with(fakeRepo("org/secret"), "id", "id-org/secret"), "isPrivate", true)),
			fakeContribution("org/both", "2026-08-01T07:00:00Z"),
			// the same day as org/newer: sorted by name
			fakeContribution("org/alike", "2026-10-10T07:00:00Z"),
			// a repository without a visible contribution must not crash
			with(fakeContribution("org/empty", "2026-08-01T07:00:00Z"), "contributions", obj{"edges": list{}}),
		},
		{
			fakeContribution("org/both", "2026-01-01T07:00:00Z"),
			fakeContribution("org/older", "2025-12-01T07:00:00Z"),
		},
	}, nil)

	got := recentContributions(10)

	var names []string
	for _, c := range got {
		names = append(names, c.Repo.Name)
	}
	want := "org/alike org/newer org/both org/older"
	if strings.Join(names, " ") != want {
		t.Fatalf("got %v, want %s", names, want)
	}
	if day := got[2].OccurredAt.Format("2006-01-02"); day != "2026-08-01" {
		t.Errorf("org/both occurred at %s, want the most recent contribution 2026-08-01", day)
	}
	if got[1].Repo.URL != "https://github.com/org/newer" || got[1].Repo.Description != "about org/newer" {
		t.Errorf("repository fields are not filled in: %+v", got[1].Repo)
	}

	if len(*requests) != contributionWindows {
		t.Fatalf("got %d queries, want one per window (%d)", len(*requests), contributionWindows)
	}
	for i, req := range *requests {
		from, err := time.Parse(time.RFC3339, req.Variables["from"].(string))
		if err != nil {
			t.Fatal(err)
		}
		to, err := time.Parse(time.RFC3339, req.Variables["to"].(string))
		if err != nil {
			t.Fatal(err)
		}
		days := to.Sub(from).Hours() / 24
		if want := float64(contributionWindowMonths) * 30.5; days < want-5 || days > want+5 {
			t.Errorf("query %d asks for %.0f days, want about %.0f", i+1, days, want)
		}
		if i > 0 && req.Variables["to"] != (*requests)[i-1].Variables["from"] {
			t.Errorf("query %d does not continue where query %d began", i+1, i)
		}
	}

	if got := recentContributions(2); len(got) != 2 {
		t.Errorf("got %d contributions, want 2", len(got))
	}
	if len(*requests) != contributionWindows {
		t.Errorf("a second call asked GitHub again: %d queries", len(*requests))
	}
}

func TestRecentReleases(t *testing.T) {
	requests := fakeContributions(t, []list{
		{
			fakeContribution("org/old-release", "2026-10-10T07:00:00Z"),
			fakeContribution("org/no-release", "2026-10-09T07:00:00Z"),
			with(fakeContribution("org/secret", "2026-10-09T07:00:00Z"), "repository",
				with(with(fakeRepo("org/secret"), "id", "id-org/secret"), "isPrivate", true)),
		},
		{
			fakeContribution("org/new-release", "2026-01-01T07:00:00Z"),
		},
	}, map[string]list{
		"org/old-release": {fakeRelease("v1.1.0", "2026-03-01T00:00:00Z"), fakeRelease("v1.0.0", "2026-02-01T00:00:00Z")},
		"org/new-release": {fakeRelease("v2.0.0", "2026-09-01T00:00:00Z")},
		"org/secret":      {fakeRelease("v9.0.0", "2026-10-01T00:00:00Z")},
	})

	got := recentReleases(10)

	var names []string
	for _, r := range got {
		names = append(names, r.Name+"@"+r.LastRelease.TagName)
	}
	want := "org/new-release@v2.0.0 org/old-release@v1.1.0"
	if strings.Join(names, " ") != want {
		t.Fatalf("got %v, want %s", names, want)
	}
	if got[0].LastRelease.URL != "https://example.org/releases/v2.0.0" || got[0].LastRelease.Name != "Release v2.0.0" {
		t.Errorf("release fields are not filled in: %+v", got[0].LastRelease)
	}
	if got[0].Description != "about org/new-release" {
		t.Errorf("repository fields are not filled in: %+v", got[0])
	}

	// one query per window and one for the releases of all repositories
	if len(*requests) != contributionWindows+1 {
		t.Fatalf("got %d queries, want %d", len(*requests), contributionWindows+1)
	}
	last := (*requests)[len(*requests)-1]
	if !strings.Contains(last.Query, "$ids:[ID!]!") {
		t.Errorf("the releases query does not declare a list of IDs: %s", last.Query)
	}
	for _, id := range last.Variables["ids"].([]interface{}) {
		if id == "id-org/secret" {
			t.Errorf("asked for the releases of a private repository")
		}
	}

	if got := recentReleases(1); len(got) != 1 || got[0].Name != "org/new-release" {
		t.Errorf("got %+v, want only org/new-release", got)
	}
}

func TestRecentReleasesBatches(t *testing.T) {
	var window list
	releases := map[string]list{}
	total := releaseBatchSize*2 + 1
	for i := 0; i < total; i++ {
		name := fmt.Sprintf("org/repo-%03d", i)
		day := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC).AddDate(0, 0, i).Format(time.RFC3339)
		window = append(window, fakeContribution(name, day))
		releases[name] = list{fakeRelease("v1", day)}
	}
	requests := fakeContributions(t, []list{window, {}}, releases)

	if got := recentReleases(total + 1); len(got) != total {
		t.Fatalf("got %d repositories, want %d", len(got), total)
	}

	var batches []int
	for _, req := range *requests {
		if ids, ok := req.Variables["ids"].([]interface{}); ok {
			batches = append(batches, len(ids))
		}
	}
	want := []int{releaseBatchSize, releaseBatchSize, 1}
	if fmt.Sprint(batches) != fmt.Sprint(want) {
		t.Errorf("got batches of %v, want %v", batches, want)
	}
}
