package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/mmcdole/gofeed"
)

const testRSS = `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0">
  <channel>
    <title>Blog</title>
    <link>https://example.org/</link>
    <description>Posts</description>
    <item>
      <title>Third post</title>
      <link>https://example.org/third</link>
      <description>About the &lt;b&gt;third&lt;/b&gt; post</description>
      <guid isPermaLink="false">post-3</guid>
      <pubDate>Mon, 12 Oct 2026 08:00:00 +0000</pubDate>
    </item>
    <item>
      <title>Second post</title>
      <link>https://example.org/second</link>
      <guid>https://example.org/second</guid>
      <pubDate>not a date</pubDate>
    </item>
    <item>
      <title>First post</title>
      <link>https://example.org/first</link>
      <description>About the first post</description>
    </item>
  </channel>
</rss>`

const testAtom = `<?xml version="1.0" encoding="UTF-8"?>
<feed xmlns="http://www.w3.org/2005/Atom">
  <title>Blog</title>
  <id>urn:blog</id>
  <updated>2026-10-12T08:00:00Z</updated>
  <entry>
    <title>Only updated</title>
    <link href="https://example.org/updated"/>
    <id>urn:blog:updated</id>
    <summary>A summary</summary>
    <updated>2026-10-11T08:00:00Z</updated>
  </entry>
</feed>`

func parseFeed(t *testing.T, content string) []*gofeed.Item {
	t.Helper()

	feed, err := gofeed.NewParser().ParseString(content)
	if err != nil {
		t.Fatalf("can't parse feed: %v", err)
	}
	return feed.Items
}

func TestRSSEntries(t *testing.T) {
	got := rssEntries(parseFeed(t, testRSS), 5)
	if len(got) != 3 {
		t.Fatalf("got %d entries, want 3", len(got))
	}

	want := RSSEntry{
		Title:       "Third post",
		Description: "About the <b>third</b> post",
		URL:         "https://example.org/third",
		GUID:        "post-3",
		PublishedAt: time.Date(2026, time.October, 12, 8, 0, 0, 0, time.UTC),
	}
	if !got[0].PublishedAt.Equal(want.PublishedAt) {
		t.Errorf("published at %s, want %s", got[0].PublishedAt, want.PublishedAt)
	}
	got[0].PublishedAt = want.PublishedAt
	if got[0] != want {
		t.Errorf("got %+v, want %+v", got[0], want)
	}

	if got[1].GUID != "https://example.org/second" || got[1].Description != "" {
		t.Errorf("got %+v, want the GUID and no description", got[1])
	}

	// neither an unreadable nor a missing date may stop the rendering
	for _, entry := range got[1:] {
		if !entry.PublishedAt.IsZero() {
			t.Errorf("%s: published at %s, want the zero time", entry.Title, entry.PublishedAt)
		}
	}
	if got[2].GUID != "" || got[2].Description != "About the first post" {
		t.Errorf("got %+v, want the description and no GUID", got[2])
	}
}

func TestRSSEntriesCount(t *testing.T) {
	items := parseFeed(t, testRSS)

	for count, want := range map[int]int{1: 1, 2: 2, 3: 3, 10: 3} {
		if got := rssEntries(items, count); len(got) != want {
			t.Errorf("count %d: got %d entries, want %d", count, len(got), want)
		}
	}
	if got := rssEntries(items, 2); got[0].Title != "Third post" || got[1].Title != "Second post" {
		t.Errorf("got %q and %q, want the first two entries of the feed", got[0].Title, got[1].Title)
	}
	if got := rssEntries(nil, 5); len(got) != 0 {
		t.Errorf("got %d entries for an empty feed", len(got))
	}
}

func TestRSSEntriesAtom(t *testing.T) {
	got := rssEntries(parseFeed(t, testAtom), 5)
	if len(got) != 1 {
		t.Fatalf("got %d entries, want 1", len(got))
	}

	if got[0].GUID != "urn:blog:updated" || got[0].Description != "A summary" || got[0].URL != "https://example.org/updated" {
		t.Errorf("got %+v", got[0])
	}
	// an Atom entry needs no date of publication, only one of its update
	want := time.Date(2026, time.October, 11, 8, 0, 0, 0, time.UTC)
	if !got[0].PublishedAt.Equal(want) {
		t.Errorf("published at %s, want %s", got[0].PublishedAt, want)
	}
}

func TestRSSFeed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/rss+xml")
		fmt.Fprint(w, testRSS)
	}))
	defer srv.Close()

	var titles []string
	for _, entry := range rssFeed(srv.URL, 2) {
		titles = append(titles, entry.Title)
	}
	if got := strings.Join(titles, ", "); got != "Third post, Second post" {
		t.Errorf("got %q, want the first two entries", got)
	}
}
