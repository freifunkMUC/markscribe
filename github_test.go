package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/shurcooL/githubv4"
)

// graphQLRequest is what the client sends to the GraphQL endpoint.
type graphQLRequest struct {
	Query     string
	Variables map[string]interface{}
}

// fakeGitHub points the GitHub client at a local server for the duration of
// a test. For every query, respond returns the value of the "data" field of
// the answer. The requests are recorded in the returned slice.
func fakeGitHub(t *testing.T, login string, respond func(req graphQLRequest) interface{}) *[]graphQLRequest {
	t.Helper()

	var requests []graphQLRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req graphQLRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("can't decode request: %v", err)
		}
		requests = append(requests, req)

		w.Header().Set("Content-Type", "application/json")
		err := json.NewEncoder(w).Encode(map[string]interface{}{"data": respond(req)})
		if err != nil {
			t.Errorf("can't encode response: %v", err)
		}
	}))

	oldClient, oldUsername := gitHubClient, username
	gitHubClient = githubv4.NewEnterpriseClient(srv.URL, srv.Client())
	username = login
	contributedReposCache = nil

	t.Cleanup(func() {
		srv.Close()
		gitHubClient, username = oldClient, oldUsername
		contributedReposCache = nil
	})

	return &requests
}

// obj is a JSON object in a faked response.
type obj = map[string]interface{}

// list is a JSON array in a faked response.
type list = []interface{}

func fakeRepo(name string) obj {
	return obj{
		"nameWithOwner": name,
		"url":           "https://github.com/" + name,
		"description":   "about " + name,
		"isPrivate":     false,
		"stargazers":    obj{"totalCount": 1},
	}
}

func with(o obj, key string, value interface{}) obj {
	c := obj{}
	for k, v := range o {
		c[k] = v
	}
	c[key] = value
	return c
}

// fakePage answers a paginated query: it cuts the page that the variables
// "count" and "after" ask for out of all and returns it as a connection.
// The cursor of an entry is its index.
func fakePage(t *testing.T, req graphQLRequest, all list) obj {
	t.Helper()

	count := int(req.Variables["count"].(float64))
	if count < 1 || count > 100 {
		t.Errorf("asked for a page of %d entries, GitHub allows 1 to 100", count)
	}

	start := 0
	if after, ok := req.Variables["after"].(string); ok {
		var index int
		if _, err := fmt.Sscanf(after, "%d", &index); err != nil {
			t.Errorf("unknown cursor %q", after)
		}
		start = index + 1
	}
	end := start + count
	if end > len(all) {
		end = len(all)
	}

	edges := list{}
	for i := start; i < end; i++ {
		edges = append(edges, obj{"cursor": fmt.Sprint(i), "node": all[i]})
	}
	pageInfo := obj{"hasNextPage": end < len(all), "endCursor": nil}
	if end > start {
		pageInfo["endCursor"] = fmt.Sprint(end - 1)
	}

	return obj{"totalCount": len(all), "pageInfo": pageInfo, "edges": edges}
}
