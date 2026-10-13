package main

import (
	"context"

	"github.com/shurcooL/githubv4"
)

var recentStarsQuery struct {
	User struct {
		Login githubv4.String
		Stars struct {
			TotalCount githubv4.Int
			PageInfo   qlPageInfo
			Edges      []struct {
				Cursor    githubv4.String
				StarredAt githubv4.DateTime
				Node      qlRepository
			}
		} `graphql:"starredRepositories(first: $count, after:$after, orderBy: {field: STARRED_AT, direction: DESC})"`
	} `graphql:"user(login:$username)"`
}

func recentStars(count int) []Star {
	var starredRepos []Star
	if count <= 0 {
		return starredRepos
	}

	var after *githubv4.String
	pageSize := count
	if pageSize > maxPageSize {
		pageSize = maxPageSize
	}
	for {
		variables := map[string]interface{}{
			"username": githubv4.String(username),
			"count":    githubv4.Int(pageSize),
			"after":    after,
		}
		err := gitHubClient.Query(context.Background(), &recentStarsQuery, variables)
		if err != nil {
			panic(err)
		}

		for _, v := range recentStarsQuery.User.Stars.Edges {
			if v.Node.IsPrivate {
				continue
			}
			starredRepos = append(starredRepos, Star{
				StarredAt: v.StarredAt.Time,
				Repo:      repoFromQL(v.Node),
			})
			if len(starredRepos) >= count {
				return starredRepos
			}
		}

		// Without this check the loop asked for the last page again and
		// again when there were fewer public stars than wanted.
		pageInfo := recentStarsQuery.User.Stars.PageInfo
		if !pageInfo.HasNextPage {
			break
		}
		after = githubv4.NewString(pageInfo.EndCursor)
		// the first page was not enough, so do not go on in small steps
		pageSize = maxPageSize
	}

	return starredRepos
}

/*
{
	viewer {
		login
		starredRepositories(first: 3, orderBy: {field: STARRED_AT, direction: DESC}) {
			totalCount
			edges {
				cursor
				starredAt
				node {
					nameWithOwner
					url
					description
				}
			}
		}
	}
}
*/
