package main

import (
	"time"

	"github.com/mmcdole/gofeed"
)

// RSSEntry represents a single RSS entry.
type RSSEntry struct {
	Title       string
	Description string
	URL         string
	GUID        string
	PublishedAt time.Time
}

func rssFeed(url string, count int) []RSSEntry {
	fp := gofeed.NewParser()
	feed, err := fp.ParseURL(url)
	if err != nil {
		panic(err)
	}

	return rssEntries(feed.Items, count)
}

// rssEntries converts the first count items of a feed.
func rssEntries(items []*gofeed.Item, count int) []RSSEntry {
	var r []RSSEntry

	for _, v := range items {
		// fmt.Printf("%+v\n", v)

		r = append(r, RSSEntry{
			Title:       v.Title,
			Description: v.Description,
			URL:         v.Link,
			GUID:        v.GUID,
			PublishedAt: rssPublishedAt(v),
		})
		if len(r) == count {
			break
		}
	}

	return r
}

// rssPublishedAt returns the date an item was published. An item without
// such a date, or with one that cannot be read, gets the date of its last
// update, and the zero time if it does not have that either.
func rssPublishedAt(item *gofeed.Item) time.Time {
	if item.PublishedParsed != nil {
		return *item.PublishedParsed
	}
	if item.UpdatedParsed != nil {
		return *item.UpdatedParsed
	}
	return time.Time{}
}
