package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const testSHA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

func TestGitHubSnapshotPaginatesAndPreservesTimelineOrder(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		var request struct {
			Query string `json:"query"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(request.Query, "$after") {
			_, _ = w.Write([]byte(`{"data":{"repository":{"pullRequest":{"commits":{"nodes":[{"commit":{"oid":"` + testSHA + `","message":"second","committedDate":"2026-01-02T00:00:00Z"}}],"pageInfo":{"hasNextPage":false}}}}}}`))
			return
		}
		_, _ = w.Write([]byte(`{"data":{"repository":{"pullRequest":{"title":"Title","body":"Body","headRefOid":"` + testSHA + `","state":"OPEN","commits":{"nodes":[],"pageInfo":{"hasNextPage":true,"endCursor":"next"}},"timelineItems":{"nodes":[{"__typename":"IssueComment","body":"first","createdAt":"2026-01-03T00:00:00Z","author":{"login":"alice"}},{"__typename":"PullRequestCommit","commit":{"oid":"` + testSHA + `","message":"second","committedDate":"2026-01-02T00:00:00Z"}}],"pageInfo":{"hasNextPage":false}},"closingIssuesReferences":{"nodes":[{"number":7,"title":"Issue","body":"Details","repository":{"nameWithOwner":"owner/repo"}}],"pageInfo":{"hasNextPage":false}}}}}}`))
	}))
	defer server.Close()
	reader, err := NewGitHub("owner/repo", server.URL, "")
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := reader.Snapshot(context.Background(), 3)
	if err != nil {
		t.Fatal(err)
	}
	if calls != 2 || len(snapshot.Commits) != 1 || snapshot.Change.LinkedIssues[0] != 7 {
		t.Fatalf("unexpected snapshot: calls=%d value=%+v", calls, snapshot)
	}
	if snapshot.Events[0].Kind != "comment" || snapshot.Events[1].Kind != "commit" {
		t.Fatalf("host order lost: %+v", snapshot.Events)
	}
}

func TestGitHubRejectsCrossRepositoryLinks(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"data":{"repository":{"pullRequest":{"title":"T","headRefOid":"` + testSHA + `","state":"OPEN","commits":{"nodes":[],"pageInfo":{"hasNextPage":false}},"timelineItems":{"nodes":[],"pageInfo":{"hasNextPage":false}},"closingIssuesReferences":{"nodes":[{"number":7,"repository":{"nameWithOwner":"other/repo"}}],"pageInfo":{"hasNextPage":false}}}}}}`))
	}))
	defer server.Close()
	reader, _ := NewGitHub("owner/repo", server.URL, "")
	if _, err := reader.Snapshot(context.Background(), 3); err != ErrIncomplete {
		t.Fatalf("want incomplete cross-repository relation, got %v", err)
	}
}

func TestGitHubIssueClosingPRAndMissingPageFailClosed(t *testing.T) {
	missingPage := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if missingPage {
			_, _ = w.Write([]byte(`{"data":{"repository":{"issue":{"number":7,"title":"Issue","closedByPullRequestsReferences":{"nodes":[]}}}}}`))
			return
		}
		_, _ = w.Write([]byte(`{"data":{"repository":{"issue":{"number":7,"title":"Issue","body":"Body","closedByPullRequestsReferences":{"nodes":[{"number":3,"repository":{"nameWithOwner":"owner/repo"}}],"pageInfo":{"hasNextPage":false}}}}}}`))
	}))
	defer server.Close()
	reader, _ := NewGitHub("owner/repo", server.URL, "")
	issue, err := reader.Issue(context.Background(), 7)
	if err != nil || len(issue.LinkedPRs) != 1 || issue.LinkedPRs[0] != 3 {
		t.Fatalf("closing PR missing: issue=%+v error=%v", issue, err)
	}
	missingPage = true
	if _, err := reader.Issue(context.Background(), 7); err != ErrIncomplete {
		t.Fatalf("missing page must fail: %v", err)
	}
}

func TestGitLabSnapshotAndIssueLinks(t *testing.T) {
	queries, rests := 0, 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/graphql") {
			queries++
			var request struct {
				Query string `json:"query"`
			}
			_ = json.NewDecoder(r.Body).Decode(&request)
			if strings.Contains(request.Query, "issue(iid:") {
				_, _ = w.Write([]byte(`{"data":{"project":{"id":"gid://gitlab/Project/10","issue":{"title":"Issue","description":"Text"}}}}`))
				return
			}
			_, _ = w.Write([]byte(`{"data":{"project":{"id":"gid://gitlab/Project/10","mergeRequest":{"title":"MR","description":"Body","diffHeadSha":"` + testSHA + `","state":"opened","commits":{"nodes":[{"sha":"` + testSHA + `","fullTitle":"Commit","committedDate":"2026-01-02T00:00:00Z"}],"pageInfo":{"hasNextPage":false}},"discussions":{"nodes":[{"notes":{"nodes":[{"body":"review","createdAt":"2026-01-01T00:00:00Z","system":false,"author":{"username":"bob"}}],"pageInfo":{"hasNextPage":false}}}],"pageInfo":{"hasNextPage":false}}}}}}`))
			return
		}
		rests++
		if strings.HasSuffix(r.URL.Path, "/closed_by") {
			_, _ = w.Write([]byte(`[{"project_id":10,"iid":3}]`))
		} else {
			_, _ = w.Write([]byte(`[{"project_id":10,"iid":7,"title":"Issue","description":"Text"}]`))
		}
	}))
	defer server.Close()
	reader := NewGitLab("group/repo", server.URL+"/api/v4", "")
	snapshot, err := reader.Snapshot(context.Background(), 3)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Change.LinkedIssues[0] != 7 || len(snapshot.Issues) != 1 || snapshot.Events[0].Kind != "comment" {
		t.Fatalf("unexpected GitLab snapshot: %+v", snapshot)
	}
	issue, err := reader.Issue(context.Background(), 7)
	if err != nil {
		t.Fatal(err)
	}
	if issue.LinkedPRs[0] != 3 || queries != 2 || rests != 2 {
		t.Fatalf("unexpected related reads: issue=%+v queries=%d rests=%d", issue, queries, rests)
	}
}

func TestGitLabRejectsCrossProjectLinks(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/graphql") {
			_, _ = w.Write([]byte(`{"data":{"project":{"id":"gid://gitlab/Project/10","issue":{"title":"Issue"}}}}`))
			return
		}
		_, _ = w.Write([]byte(`[{"project_id":11,"iid":3}]`))
	}))
	defer server.Close()
	reader := NewGitLab("group/repo", server.URL+"/api/v4", "")
	if _, err := reader.Issue(context.Background(), 7); err != ErrIncomplete {
		t.Fatalf("cross-project link must fail: %v", err)
	}
}
