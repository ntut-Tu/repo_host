"""GitHub GraphQL resource reader; independent of the Agent."""

from __future__ import annotations

from codehost.cache import ResourceCache
from codehost.errors import IncompleteResponseError, RemoteError
from codehost.http import HttpClient
from codehost.resources import (
    Change,
    Commit,
    Issue,
    PullRequestTimeline,
    TimelineEvent,
    require_sha,
)

_FIELDS = {
    "commits": "nodes { commit { oid message committedDate } }",
    "timelineItems": (
        "nodes { __typename "
        "... on PullRequestCommit { commit { oid message committedDate } } "
        "... on IssueComment { body createdAt author { login } } "
        "... on PullRequestReview { body submittedAt author { login } } "
        "... on PullRequestReviewThread { comments(first:100) { nodes { body createdAt author { login } } "
        "pageInfo { hasNextPage } } } "
        "... on MergedEvent { createdAt } }"
    ),
    "closingIssuesReferences": "nodes { number title body repository { nameWithOwner } }",
}
_PAGE = "pageInfo { hasNextPage endCursor }"
_BASE = "number title body headRefOid state mergedAt"
_TIMELINE_FILTER = ("itemTypes:[PULL_REQUEST_COMMIT,ISSUE_COMMENT,PULL_REQUEST_REVIEW,"
                    "PULL_REQUEST_REVIEW_THREAD,MERGED_EVENT],")


def graphql_url(api_url: str) -> str:
    root = api_url.rstrip("/")
    return root[:-7] + "/api/graphql" if root.endswith("/api/v3") else root + "/graphql"


class GitHubResources:
    """Fetch related PR resources together and cache complete snapshots."""

    def __init__(self, repository: str, headers: dict[str, str], client: HttpClient,
                 api_url: str, cache: ResourceCache | None = None) -> None:
        self.owner, self.name = repository.split("/")
        self.client = client
        self.headers = headers
        self.url = graphql_url(api_url)
        self.cache = cache or ResourceCache()

    def _query(self, query: str, **variables) -> dict:
        response = self.client.post_json(self.url, {"query": query, "variables": variables}, self.headers).json()
        if not isinstance(response, dict) or response.get("errors") or not isinstance(response.get("data"), dict):
            raise RemoteError("GitHub GraphQL request failed or returned incomplete data")
        return response["data"]

    def get_commit(self, sha: str) -> Commit:
        require_sha(sha)
        cached = self.cache.get("commit", sha)
        if cached is not None:
            return cached
        query = "query($owner:String!,$name:String!,$sha:String!){repository(owner:$owner,name:$name){object(expression:$sha){... on Commit{oid message committedDate}}}}"
        repo = self._query(query, owner=self.owner, name=self.name, sha=sha).get("repository") or {}
        item = repo.get("object") or {}
        if item.get("oid") != sha:
            raise RemoteError("Requested GitHub commit SHA is unavailable")
        commit = Commit(sha, item["message"], item.get("committedDate"))
        self.cache.put("commit", sha, commit, immutable=True)
        return commit

    def _page(self, number: int, field: str, cursor: str | None = None) -> dict:
        if field not in _FIELDS:
            raise ValueError("Unsupported PR resource")
        after = ",after:$after" if cursor is not None else ""
        variable = ",$after:String!" if cursor is not None else ""
        filters = _TIMELINE_FILTER if field == "timelineItems" else ""
        selection = f"{field}({filters}first:100{after}){{{_FIELDS[field]} {_PAGE}}}"
        query = (f"query($owner:String!,$name:String!,$number:Int!{variable})"
                 f"{{repository(owner:$owner,name:$name){{pullRequest(number:$number){{{selection}}}}}}}")
        values = {"owner": self.owner, "name": self.name, "number": number}
        if cursor is not None:
            values["after"] = cursor
        repo = self._query(query, **values).get("repository") or {}
        pr = repo.get("pullRequest") or {}
        if field not in pr:
            raise IncompleteResponseError(f"GitHub PR {field} is unavailable")
        return pr[field]

    def _all(self, number: int, field: str, first: dict) -> list[dict]:
        nodes = list(first.get("nodes") or [])
        page = first.get("pageInfo") or {}
        while page.get("hasNextPage"):
            cursor = page.get("endCursor")
            if not cursor:
                raise IncompleteResponseError(f"GitHub PR {field} cursor is missing")
            next_page = self._page(number, field, cursor)
            nodes.extend(next_page.get("nodes") or [])
            page = next_page.get("pageInfo") or {}
        return nodes

    def _snapshot(self, number: int) -> tuple[Change, tuple[Commit, ...], tuple[TimelineEvent, ...], str | None]:
        cached = self.cache.get("pr_snapshot", str(number))
        if cached is not None:
            return cached
        fields = []
        for key, value in _FIELDS.items():
            filters = _TIMELINE_FILTER if key == "timelineItems" else ""
            fields.append(f"{key}({filters}first:100){{{value} {_PAGE}}}")
        fields = " ".join(fields)
        query = ("query($owner:String!,$name:String!,$number:Int!){repository(owner:$owner,name:$name)"
                 f"{{pullRequest(number:$number){{{_BASE} {fields}}}}}}}")
        repo = self._query(query, owner=self.owner, name=self.name, number=number).get("repository") or {}
        pr = repo.get("pullRequest")
        if not pr or not pr.get("headRefOid"):
            raise RemoteError("GitHub PR or head SHA is unavailable")
        lists = {key: self._all(number, key, pr[key]) for key in _FIELDS}
        snapshot = self._build_snapshot(number, pr, lists)
        self.cache.put("pr_snapshot", str(number), snapshot)
        return snapshot

    def _build_snapshot(self, number: int, pr: dict, lists: dict) -> tuple:
        issues = lists["closingIssuesReferences"]
        if any((item.get("repository") or {}).get("nameWithOwner") !=
               f"{self.owner}/{self.name}" for item in issues):
            raise IncompleteResponseError("GitHub closing issue is outside the configured repository")
        change = Change(number, pr["title"], pr.get("body") or "", pr["headRefOid"],
                        pr["state"], tuple(item["number"] for item in issues))
        commits = tuple(Commit(item["commit"]["oid"], item["commit"]["message"],
                               item["commit"].get("committedDate")) for item in lists["commits"])
        for commit in commits:
            self.cache.put("commit", commit.sha, commit, immutable=True)
        for item in issues:
            self.cache.put("issue_summary", str(item["number"]),
                           Issue(item["number"], item["title"], item.get("body") or ""))
        events = self._events(lists["timelineItems"])
        if pr.get("mergedAt") and not any(event.kind == "merged" for event in events):
            events.append(TimelineEvent("merged", pr["mergedAt"]))
        return change, commits, tuple(events), pr.get("mergedAt")

    def _events(self, nodes: list[dict]) -> list[TimelineEvent]:
        events = []
        for item in nodes:
            kind = item.get("__typename")
            if kind == "PullRequestCommit":
                commit = item["commit"]
                events.append(TimelineEvent("commit", commit["committedDate"],
                                            commit["oid"], commit["message"]))
            elif kind == "MergedEvent":
                events.append(TimelineEvent("merged", item["createdAt"]))
            elif kind == "PullRequestReviewThread":
                connection = item.get("comments") or {}
                if (connection.get("pageInfo") or {}).get("hasNextPage"):
                    raise IncompleteResponseError("GitHub review thread exceeds one page")
                events.extend(self._comment(note, "createdAt") for note in connection.get("nodes") or [])
            elif kind in ("IssueComment", "PullRequestReview") and item.get("body"):
                field = "submittedAt" if kind == "PullRequestReview" else "createdAt"
                if item.get(field):
                    events.append(self._comment(item, field))
        return events

    @staticmethod
    def _comment(item: dict, field: str) -> TimelineEvent:
        return TimelineEvent("comment", item[field], message=item.get("body") or "",
                             author=(item.get("author") or {}).get("login"))

    def get_pr(self, number: int) -> Change:
        return self._snapshot(number)[0]

    def get_pr_timeline(self, number: int) -> PullRequestTimeline:
        change, _, events, _ = self._snapshot(number)
        return PullRequestTimeline(change, events, "GitHub PR timeline order")

    def get_issue(self, number: int) -> Issue:
        cached = self.cache.get("issue", str(number))
        if cached is not None:
            return cached
        query = ("query($owner:String!,$name:String!,$number:Int!){repository(owner:$owner,name:$name)"
                 "{issue(number:$number){number title body closedByPullRequestsReferences(first:100,includeClosedPrs:true)"
                 "{nodes{number repository{nameWithOwner}} pageInfo{hasNextPage}}}}}")
        repo = self._query(query, owner=self.owner, name=self.name, number=number).get("repository") or {}
        data = repo.get("issue")
        if not data:
            raise RemoteError("GitHub issue is unavailable")
        refs = data["closedByPullRequestsReferences"]
        if refs["pageInfo"]["hasNextPage"]:
            raise IncompleteResponseError("GitHub issue PR links exceed one page")
        if any((item.get("repository") or {}).get("nameWithOwner") !=
               f"{self.owner}/{self.name}" for item in refs["nodes"]):
            raise IncompleteResponseError("GitHub closing PR is outside the configured repository")
        issue = Issue(number, data["title"], data.get("body") or "",
                      tuple(item["number"] for item in refs["nodes"]))
        self.cache.put("issue", str(number), issue)
        return issue
