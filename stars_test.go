package main

import (
	"strings"
	"testing"
)

func starNames(stars []Star) string {
	var names []string
	for _, s := range stars {
		names = append(names, s.Repo.Name)
	}
	return strings.Join(names, " ")
}

func TestRecentStars(t *testing.T) {
	all := list{
		fakeRepo("a/one"),
		fakeRepo("a/two"),
		with(fakeRepo("a/secret"), "isPrivate", true),
		fakeRepo("a/three"),
		fakeRepo("a/four"),
	}

	requests := fakeGitHub(t, "octocat", func(req graphQLRequest) interface{} {
		return obj{"user": obj{"starredRepositories": fakePage(t, req, all)}}
	})

	// enough on the first page: a single query
	if names := starNames(recentStars(2)); names != "a/one a/two" {
		t.Errorf("got stars %s, want a/one a/two", names)
	}
	if len(*requests) != 1 {
		t.Errorf("got %d queries, want 1", len(*requests))
	}

	// a private one in between: the next page is asked for
	*requests = nil
	if names := starNames(recentStars(3)); names != "a/one a/two a/three" {
		t.Errorf("got stars %s, want a/one a/two a/three", names)
	}
	if len(*requests) != 2 {
		t.Errorf("got %d queries, want 2", len(*requests))
	}

	// more than there are: everything public, and an end instead of asking
	// for the last page forever
	*requests = nil
	if names := starNames(recentStars(10)); names != "a/one a/two a/three a/four" {
		t.Errorf("got stars %s, want all four public ones", names)
	}
	if len(*requests) != 1 {
		t.Errorf("got %d queries, want 1", len(*requests))
	}

	*requests = nil
	if got := recentStars(0); len(got) != 0 || len(*requests) != 0 {
		t.Errorf("got %d stars and %d queries for a count of 0", len(got), len(*requests))
	}
}

func TestRecentStarsWithoutAny(t *testing.T) {
	fakeGitHub(t, "octocat", func(req graphQLRequest) interface{} {
		return obj{"user": obj{"starredRepositories": fakePage(t, req, nil)}}
	})

	if got := recentStars(5); len(got) != 0 {
		t.Errorf("got %d stars, want none", len(got))
	}
}
