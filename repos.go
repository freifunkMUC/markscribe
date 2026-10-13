package main

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/shurcooL/githubv4"
)

const (
	// contributionWindows times contributionWindowMonths is the period
	// that recentContributions and recentReleases look back on. GitHub
	// allows at most one year per contributionsCollection.
	contributionWindows      = 4
	contributionWindowMonths = 3

	// releaseBatchSize is the number of repositories whose releases are
	// requested in a single query.
	releaseBatchSize = 50
)

var recentContributionsQuery struct {
	User struct {
		Login                   githubv4.String
		ContributionsCollection struct {
			CommitContributionsByRepository []struct {
				Contributions struct {
					Edges []struct {
						Cursor githubv4.String
						Node   struct {
							OccurredAt githubv4.DateTime
						}
					}
				} `graphql:"contributions(first: 1)"`
				Repository struct {
					ID githubv4.ID
					qlRepository
				}
			} `graphql:"commitContributionsByRepository(maxRepositories: 100)"`
		} `graphql:"contributionsCollection(from: $from, to: $to)"`
	} `graphql:"user(login:$username)"`
}

var recentPullRequestsQuery struct {
	User struct {
		Login        githubv4.String
		PullRequests struct {
			TotalCount githubv4.Int
			Edges      []struct {
				Cursor githubv4.String
				Node   qlPullRequest
			}
		} `graphql:"pullRequests(first: $count, orderBy: {field: CREATED_AT, direction: DESC})"`
	} `graphql:"user(login:$username)"`
}

var recentReposQuery struct {
	User struct {
		Login        githubv4.String
		Repositories struct {
			TotalCount githubv4.Int
			Edges      []struct {
				Cursor githubv4.String
				Node   qlRepository
			}
		} `graphql:"repositories(first: $count, privacy: PUBLIC, isFork: $isFork, ownerAffiliations: OWNER, orderBy: {field: CREATED_AT, direction: DESC})"`
	} `graphql:"user(login:$username)"`
}

var repoReleasesQuery struct {
	Nodes []struct {
		Repository struct {
			ID       githubv4.ID
			Releases qlRelease `graphql:"releases(first: 10, orderBy: {field: CREATED_AT, direction: DESC})"`
		} `graphql:"... on Repository"`
	} `graphql:"nodes(ids: $ids)"`
}

var repoQuery struct {
	Repository struct {
		Description   githubv4.String
		NameWithOwner githubv4.String
		IsPrivate     githubv4.Boolean
		URL           githubv4.String
		Stargazers    struct {
			TotalCount githubv4.Int
		}
		Releases qlRelease `graphql:"releases(last: 1)"`
	} `graphql:"repository(owner:$owner, name:$name)"`
}

// repoContribution is a repository the user committed to and the time of
// the most recent commit contribution to it.
type repoContribution struct {
	ID         githubv4.ID
	Repository qlRepository
	OccurredAt time.Time
}

type timeWindow struct {
	From time.Time
	To   time.Time
}

// contributedReposCache keeps the result of contributedRepos, so that a
// template that calls both recentContributions and recentReleases asks
// GitHub only once.
var contributedReposCache []repoContribution

// windowsBefore splits the period that ends at now into count consecutive
// windows of the given number of months, most recent first.
func windowsBefore(now time.Time, count, months int) []timeWindow {
	windows := make([]timeWindow, 0, count)
	for i := 0; i < count; i++ {
		windows = append(windows, timeWindow{
			From: now.AddDate(0, -(i+1)*months, 0),
			To:   now.AddDate(0, -i*months, 0),
		})
	}
	return windows
}

// mergeContributions adds the contributions in add to merged and returns
// the result. Each repository is kept once, with its most recent
// contribution.
func mergeContributions(merged, add []repoContribution) []repoContribution {
	index := make(map[githubv4.String]int, len(merged))
	for i, c := range merged {
		index[c.Repository.NameWithOwner] = i
	}

	for _, c := range add {
		if i, ok := index[c.Repository.NameWithOwner]; ok {
			if c.OccurredAt.After(merged[i].OccurredAt) {
				merged[i] = c
			}
			continue
		}
		index[c.Repository.NameWithOwner] = len(merged)
		merged = append(merged, c)
	}

	return merged
}

// contributedRepos returns the repositories the user committed to over the
// past year, most recent contribution first.
//
// GitHub limits the resources a single GraphQL query may use. For accounts
// with many contributions the default window of contributionsCollection,
// one year, exceeds that limit and the query fails, whatever it selects. So
// the year is queried in shorter windows and the results are merged.
func contributedRepos() []repoContribution {
	if contributedReposCache != nil {
		return contributedReposCache
	}

	contributions := []repoContribution{}
	for _, w := range windowsBefore(time.Now(), contributionWindows, contributionWindowMonths) {
		variables := map[string]interface{}{
			"username": githubv4.String(username),
			"from":     githubv4.DateTime{Time: w.From},
			"to":       githubv4.DateTime{Time: w.To},
		}
		err := gitHubClient.Query(context.Background(), &recentContributionsQuery, variables)
		if err != nil {
			panic(err)
		}

		var window []repoContribution
		for _, v := range recentContributionsQuery.User.ContributionsCollection.CommitContributionsByRepository {
			if len(v.Contributions.Edges) == 0 {
				continue
			}

			window = append(window, repoContribution{
				ID:         v.Repository.ID,
				Repository: v.Repository.qlRepository,
				OccurredAt: v.Contributions.Edges[0].Node.OccurredAt.Time,
			})
		}
		contributions = mergeContributions(contributions, window)
	}

	// Contributions are dated by the day. Repositories with the same day
	// are sorted by name, so that their order does not depend on the order
	// within a window, which changes as the windows move on.
	sort.Slice(contributions, func(i, j int) bool {
		if contributions[i].OccurredAt.Equal(contributions[j].OccurredAt) {
			return contributions[i].Repository.NameWithOwner < contributions[j].Repository.NameWithOwner
		}
		return contributions[i].OccurredAt.After(contributions[j].OccurredAt)
	})

	contributedReposCache = contributions
	return contributions
}

func recentContributions(count int) []Contribution {
	// fmt.Printf("Finding recent contributions...\n")

	var contributions []Contribution
	for _, v := range contributedRepos() {
		// ignore meta-repo
		if string(v.Repository.NameWithOwner) == fmt.Sprintf("%s/%s", username, username) {
			continue
		}
		if v.Repository.IsPrivate {
			continue
		}

		contributions = append(contributions, Contribution{
			Repo:       repoFromQL(v.Repository),
			OccurredAt: v.OccurredAt,
		})
	}

	// fmt.Printf("Found %d contributions!\n", len(repos))
	if len(contributions) > count {
		return contributions[:count]
	}
	return contributions
}

func recentPullRequests(count int) []PullRequest {
	// fmt.Printf("Finding recently created pullRequests...\n")

	var pullRequests []PullRequest
	variables := map[string]interface{}{
		"username": githubv4.String(username),
		"count":    githubv4.Int(count + 1), // +1 in case we encounter the meta-repo itself
	}
	err := gitHubClient.Query(context.Background(), &recentPullRequestsQuery, variables)
	if err != nil {
		panic(err)
	}

	for _, v := range recentPullRequestsQuery.User.PullRequests.Edges {
		// ignore meta-repo
		if string(v.Node.Repository.NameWithOwner) == fmt.Sprintf("%s/%s", username, username) {
			continue
		}
		if v.Node.Repository.IsPrivate {
			continue
		}

		pullRequests = append(pullRequests, pullRequestFromQL(v.Node))
		if len(pullRequests) == count {
			break
		}
	}

	// fmt.Printf("Found %d pullRequests!\n", len(pullRequests))
	return pullRequests
}

func recentRepos(count int) []Repo {
	// fmt.Printf("Finding recently created repos...\n")

	var repos []Repo
	variables := map[string]interface{}{
		"username": githubv4.String(username),
		"count":    githubv4.Int(count + 1), // +1 in case we encounter the meta-repo itself
		"isFork":   githubv4.Boolean(false),
	}
	err := gitHubClient.Query(context.Background(), &recentReposQuery, variables)
	if err != nil {
		panic(err)
	}

	for _, v := range recentReposQuery.User.Repositories.Edges {
		// ignore meta-repo
		if string(v.Node.NameWithOwner) == fmt.Sprintf("%s/%s", username, username) {
			continue
		}

		repos = append(repos, repoFromQL(v.Node))
		if len(repos) == count {
			break
		}
	}

	// fmt.Printf("Found %d repos!\n", len(repos))
	return repos
}

func recentForks(count int) []Repo {
	// fmt.Printf("Finding recently created repos...\n")

	var repos []Repo
	variables := map[string]interface{}{
		"username": githubv4.String(username),
		"count":    githubv4.Int(count + 1), // +1 in case we encounter the meta-repo itself
		"isFork":   githubv4.Boolean(true),
	}
	err := gitHubClient.Query(context.Background(), &recentReposQuery, variables)
	if err != nil {
		panic(err)
	}

	for _, v := range recentReposQuery.User.Repositories.Edges {
		// ignore meta-repo
		if string(v.Node.NameWithOwner) == fmt.Sprintf("%s/%s", username, username) {
			continue
		}

		repos = append(repos, repoFromQL(v.Node))
		if len(repos) == count {
			break
		}
	}

	// fmt.Printf("Found %d repos!\n", len(repos))
	return repos
}

// repoReleases returns the most recent releases of the repositories with
// the given node IDs.
func repoReleases(ids []githubv4.ID) map[githubv4.ID]qlRelease {
	releases := make(map[githubv4.ID]qlRelease, len(ids))

	for len(ids) > 0 {
		batch := ids
		if len(batch) > releaseBatchSize {
			batch = batch[:releaseBatchSize]
		}
		ids = ids[len(batch):]

		variables := map[string]interface{}{
			"ids": batch,
		}
		err := gitHubClient.Query(context.Background(), &repoReleasesQuery, variables)
		if err != nil {
			panic(err)
		}

		for _, v := range repoReleasesQuery.Nodes {
			// a repository that cannot be read comes back empty
			if v.Repository.ID == nil {
				continue
			}
			releases[v.Repository.ID] = v.Repository.Releases
		}
	}

	return releases
}

// recentReleases returns the public repositories the user committed to over
// the past year that have a release, most recent release first.
//
// It used to ask for repositoriesContributedTo with the releases of every
// repository in one query. For accounts with many contributions that query
// runs into GitHub's resource limits, so the repositories now come from the
// contributions and their releases are requested in batches.
func recentReleases(count int) []Repo {
	// fmt.Printf("Finding recent releases...\n")

	var contributions []repoContribution
	var ids []githubv4.ID
	for _, c := range contributedRepos() {
		if c.Repository.IsPrivate {
			continue
		}
		contributions = append(contributions, c)
		ids = append(ids, c.ID)
	}
	releases := repoReleases(ids)

	var repos []Repo
	for _, c := range contributions {
		r := repoFromQL(c.Repository)
		rels := releases[c.ID]

		for _, rel := range rels.Nodes {
			if rel.IsPrerelease || rel.IsDraft {
				continue
			}
			if rels.Nodes[0].TagName == "" ||
				rels.Nodes[0].PublishedAt.Time.IsZero() {
				continue
			}
			r.LastRelease = releaseFromQL(rels)
			break
		}

		if !r.LastRelease.PublishedAt.IsZero() {
			repos = append(repos, r)
		}
	}

	sort.Slice(repos, func(i, j int) bool {
		if repos[i].LastRelease.PublishedAt.Equal(repos[j].LastRelease.PublishedAt) {
			return repos[i].Stargazers > repos[j].Stargazers
		}
		return repos[i].LastRelease.PublishedAt.After(repos[j].LastRelease.PublishedAt)
	})

	// fmt.Printf("Found %d repos!\n", len(repos))
	if len(repos) > count {
		return repos[:count]
	}
	return repos
}

func repo(owner, name string) Repo {
	variables := map[string]interface{}{
		"owner": githubv4.String(owner),
		"name":  githubv4.String(name),
	}
	err := gitHubClient.Query(context.Background(), &repoQuery, variables)
	if err != nil {
		panic(err)
	}
	repo := repoQuery.Repository
	return Repo{
		Name:        string(repo.NameWithOwner),
		URL:         string(repo.URL),
		Description: string(repo.Description),
		Stargazers:  int(repo.Stargazers.TotalCount),
		IsPrivate:   bool(repo.IsPrivate),
		LastRelease: releaseFromQL(repo.Releases),
	}
}

/*
{
  user(login: "muesli") {
    login
    repositoriesContributedTo(first: 100, includeUserRepositories: true, contributionTypes: COMMIT) {
      totalCount
      edges {
        cursor
        node {
          id
          nameWithOwner
        }
      }
    }
  }
}

{
  user(login: "muesli") {
    login
    repositoriesContributedTo(first: 100, includeUserRepositories: true, contributionTypes: COMMIT) {
      totalCount
      edges {
        cursor
        node {
          id
          nameWithOwner
		  releases(first: 3, orderBy: {field: CREATED_AT, direction: DESC}) {
          	nodes {
          	  name
              PublishedAt
			  url
			  isPrerelease
			  isDraft
            }
          }
        }
      }
    }
  }
}

{
  user(login: "muesli") {
    login
    repositories(first: 10, privacy: PUBLIC, isFork: false, ownerAffiliations: OWNER, orderBy: {field: CREATED_AT, direction: DESC}) {
      totalCount
      edges {
        cursor
        node {
          id
          nameWithOwner
        }
      }
    }
  }
}

{
  user(login: "muesli") {
    login
    contributionsCollection {
      commitContributionsByRepository {
        contributions(first: 1) {
          edges {
            cursor
            node {
              occurredAt
            }
          }
        }
        repository {
          id
		  nameWithOwner
		  url
		  description
        }
      }
    }
  }
}
*/
