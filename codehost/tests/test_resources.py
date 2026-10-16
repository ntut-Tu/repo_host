"""Resource use cases and bundled-fetch behavior for the package design."""

import json
import unittest

from codehost.errors import IncompleteResponseError, RemoteError
from codehost.github import GitHubApi
from codehost.gitlab import GitLabApi
from codehost.http import Response


def connection(nodes, more=False, cursor=None):
    return {"nodes": nodes, "pageInfo": {"hasNextPage": more, "endCursor": cursor}}


class FakeHttp:
    def __init__(self, graphql, rest=None):
        self.graphql = graphql
        self.rest = rest or {}
        self.posts = []
        self.gets = []

    def post_json(self, url, payload, headers=None):
        self.posts.append((url, payload))
        data = self.graphql(payload)
        return Response(json.dumps({"data": data}).encode(), {})

    def get(self, url, headers=None):
        self.gets.append(url)
        return Response(json.dumps(self.rest[url]).encode(), {})


class ResourceUseCases(unittest.TestCase):
    def test_github_bundles_thirty_commits_five_comments_and_issue(self):
        commits = [{"commit": {"oid": f"{n:040x}", "message": f"commit {n}",
                               "committedDate": f"2026-01-01T00:{n:02}:00Z"}} for n in range(30)]
        comments = [{"body": f"feedback {n}", "createdAt": f"2026-01-01T01:{n:02}:00Z",
                     "author": {"login": "reviewer"}} for n in range(5)]
        timeline_nodes = ([{"__typename": "PullRequestCommit", **item} for item in commits[:10]] +
                          [{"__typename": "IssueComment", **item} for item in comments] +
                          [{"__typename": "PullRequestCommit", **item} for item in commits[10:]] +
                          [{"__typename": "MergedEvent", "createdAt": "2026-01-02T00:00:00Z"}])
        pr = {"title": "Feature", "body": "description", "headRefOid": commits[-1]["commit"]["oid"],
              "state": "MERGED", "mergedAt": "2026-01-02T00:00:00Z",
              "commits": connection(commits), "timelineItems": connection(timeline_nodes),
              "closingIssuesReferences": connection([{"number": 7, "title": "Issue", "body": "details",
                                                      "repository": {"nameWithOwner": "owner/repo"}}])}
        http = FakeHttp(lambda payload: {"repository": {"pullRequest": pr}})
        api = GitHubApi("owner/repo", "token", http)
        timeline = api.get_pr_timeline(4)
        self.assertEqual((len(timeline.events), timeline.change.linked_issues), (36, (7,)))
        self.assertEqual((timeline.events[10].kind, timeline.events[15].kind), ("comment", "commit"))
        self.assertEqual(api.get_commit(commits[0]["commit"]["oid"]).message, "commit 0")
        self.assertEqual(api.get_pr(4).head_sha, commits[-1]["commit"]["oid"])
        self.assertEqual(len(http.posts), 1)
        self.assertIn("closingIssuesReferences", http.posts[0][1]["query"])

    def test_github_issue_and_exact_commit_are_independent(self):
        sha = "a" * 40

        def graphql(payload):
            query = payload["query"]
            if "object(expression" in query:
                return {"repository": {"object": {"oid": sha, "message": "fix", "committedDate": None}}}
            return {"repository": {"issue": {"title": "Bug", "body": "description",
                    "closedByPullRequestsReferences": connection([{"number": 3,
                        "repository": {"nameWithOwner": "owner/repo"}}])}}}

        http = FakeHttp(graphql)
        api = GitHubApi("owner/repo", None, http)
        self.assertEqual(api.get_issue(7).linked_prs, (3,))
        self.assertEqual(api.get_commit(sha).message, "fix")
        self.assertEqual(api.get_commit(sha).sha, sha)
        self.assertEqual(len(http.posts), 2)

    def test_gitlab_bundles_history_and_fetches_issue_links(self):
        sha = "b" * 40
        root = "https://gitlab.com/api/v4/projects/group%2Frepo"
        mr = {"title": "Feature", "description": "description", "diffHeadSha": sha,
              "state": "merged", "mergedAt": "2026-01-02T00:00:00Z",
              "commits": connection([{"sha": sha, "fullTitle": "fix", "description": "detail",
                                      "committedDate": "2026-01-01T00:00:00Z"}]),
              "discussions": connection([{"notes": connection([{"body": "please fix", "system": False,
                           "createdAt": "2026-01-01T01:00:00Z", "author": {"username": "reviewer"}}])}])}
        rest = {root + "/merge_requests/4/closes_issues?per_page=100&page=1":
                [{"iid": 7, "project_id": 1, "title": "Issue", "description": "details"}],
                root + "/issues/7/closed_by?per_page=100&page=1": [{"iid": 4, "project_id": 1}]}
        def graphql(payload):
            if "issue(iid:" in payload["query"]:
                return {"project": {"id": "gid://gitlab/Project/1",
                                    "issue": {"title": "Issue", "description": "details"}}}
            return {"project": {"id": "gid://gitlab/Project/1", "mergeRequest": mr}}
        http = FakeHttp(graphql, rest)
        api = GitLabApi("group/repo", None, http)
        timeline = api.get_pr_timeline(4)
        self.assertEqual([item.kind for item in timeline.events], ["commit", "comment", "merged"])
        self.assertEqual(api.get_pr(4).linked_issues, (7,))
        self.assertEqual(api.get_commit(sha).message, "fix\n\ndetail")
        self.assertEqual(api.get_issue(7).linked_prs, (4,))
        self.assertEqual(len(http.posts), 2)
        self.assertEqual(http.posts[0][0], "https://gitlab.com/api/graphql")

    def test_github_paginates_history_and_rejects_incomplete_review_thread(self):
        first_sha, second_sha = "1" * 40, "2" * 40
        first_commit = {"oid": first_sha, "message": "first", "committedDate": "2026-01-01T00:00:00Z"}
        second_commit = {"oid": second_sha, "message": "second", "committedDate": "2026-01-02T00:00:00Z"}
        first = {"title": "Change", "body": "", "headRefOid": second_sha, "state": "OPEN",
                 "mergedAt": None, "commits": connection([{"commit": first_commit}], True, "commit-next"),
                 "timelineItems": connection([{"__typename": "PullRequestCommit", "commit": first_commit}],
                                             True, "timeline-next"),
                 "closingIssuesReferences": connection([])}

        def graphql(payload):
            cursor = payload["variables"].get("after")
            if cursor == "commit-next":
                return {"repository": {"pullRequest": {"commits": connection([{"commit": second_commit}])}}}
            if cursor == "timeline-next":
                return {"repository": {"pullRequest": {"timelineItems": connection([
                    {"__typename": "PullRequestCommit", "commit": second_commit}])}}}
            return {"repository": {"pullRequest": first}}

        api = GitHubApi("owner/repo", None, FakeHttp(graphql))
        self.assertEqual([event.sha for event in api.get_pr_timeline(4).events], [first_sha, second_sha])
        self.assertEqual(api.get_commit(second_sha).message, "second")
        first["timelineItems"] = connection([{"__typename": "PullRequestReviewThread",
            "comments": connection([], True, "more")}])
        with self.assertRaises(IncompleteResponseError):
            GitHubApi("owner/repo", None, FakeHttp(graphql)).get_pr_timeline(4)

    def test_gitlab_paginates_commits_and_skips_system_notes(self):
        sha = "a" * 40
        root = "https://gitlab.com/api/v4/projects/group%2Frepo"
        first = {"title": "Change", "description": "", "diffHeadSha": sha, "state": "opened",
                 "mergedAt": None, "commits": connection([], True, "commits-next"),
                 "discussions": connection([], True, "discussion-next")}

        def graphql(payload):
            cursor = payload["variables"].get("after")
            if cursor == "commits-next":
                value = {"commits": connection([{"sha": sha, "fullTitle": "fix",
                       "description": None, "committedDate": "2026-01-01T00:00:00Z"}])}
            elif cursor == "discussion-next":
                notes = connection([{"body": "internal activity", "system": True,
                                     "createdAt": "2026-01-01T01:00:00Z"}])
                value = {"discussions": connection([{"notes": notes}])}
            else:
                value = first
            return {"project": {"id": "gid://gitlab/Project/1", "mergeRequest": value}}

        rest = {root + "/merge_requests/4/closes_issues?per_page=100&page=1": []}
        api = GitLabApi("group/repo", None, FakeHttp(graphql, rest))
        self.assertEqual([event.kind for event in api.get_pr_timeline(4).events], ["commit"])
        self.assertEqual(api.get_commit(sha).message, "fix")

    def test_graphql_errors_do_not_return_partial_resources(self):
        class Failed:
            def post_json(self, url, payload, headers=None):
                return Response(b'{"data":{},"errors":[{"message":"forbidden"}]}', {})

        with self.assertRaises(RemoteError):
            GitHubApi("owner/repo", None, Failed()).get_pr(4)

    def test_cross_repository_links_fail_instead_of_returning_ambiguous_numbers(self):
        github_data = {"repository": {"issue": {"title": "Bug", "body": "",
                       "closedByPullRequestsReferences": connection([{"number": 3,
                           "repository": {"nameWithOwner": "other/repo"}}])}}}
        with self.assertRaises(IncompleteResponseError):
            GitHubApi("owner/repo", None, FakeHttp(lambda payload: github_data)).get_issue(7)

        root = "https://gitlab.com/api/v4/projects/group%2Frepo"
        rest = {root + "/issues/7/closed_by?per_page=100&page=1": [{"iid": 4, "project_id": 2}]}
        graphql = lambda payload: {"project": {"id": "gid://gitlab/Project/1",
                       "issue": {"title": "Bug", "description": ""}}}
        with self.assertRaises(IncompleteResponseError):
            GitLabApi("group/repo", None, FakeHttp(graphql, rest)).get_issue(7)
