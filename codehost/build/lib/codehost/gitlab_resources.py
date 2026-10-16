"""GitLab resource reader using GraphQL for MR history and REST for links."""

from __future__ import annotations

from codehost.cache import ResourceCache
from codehost.errors import IncompleteResponseError, RemoteError
from codehost.resources import (
    Change,
    Commit,
    Issue,
    PullRequestTimeline,
    TimelineEvent,
    require_sha,
)
from codehost.timeline import make_timeline

_COMMIT = "nodes { sha fullTitle description committedDate } pageInfo { hasNextPage endCursor }"
_DISCUSSION = ("nodes { notes(first:100) { nodes { body createdAt system author { username } } "
               "pageInfo { hasNextPage } } } pageInfo { hasNextPage endCursor }")


class GitLabResources:
    def __init__(self, api, cache: ResourceCache | None = None) -> None:
        self.api = api
        self.cache = cache or ResourceCache()
        root = api.api_url.rstrip("/")
        self.url = root[:-3] + "/graphql" if root.endswith("/v4") else root + "/graphql"

    def _query(self, query: str, **variables) -> dict:
        payload = {"query": query, "variables": variables}
        response = self.api.client.post_json(self.url, payload, self.api.headers).json()
        if not isinstance(response, dict) or response.get("errors") or not isinstance(response.get("data"), dict):
            raise RemoteError("GitLab GraphQL request failed or returned incomplete data")
        return response["data"]

    def _mr(self, number: int, field: str | None = None, cursor: str | None = None) -> dict:
        if field not in (None, "commits", "discussions"):
            raise ValueError("Unsupported MR resource")
        after = ",after:$after" if cursor is not None else ""
        variable = ",$after:String!" if cursor is not None else ""
        commits = f"commits(first:100{after}){{{_COMMIT}}}" if field in (None, "commits") else ""
        discussions = (f"discussions(first:100{after}){{{_DISCUSSION}}}"
                       if field in (None, "discussions") else "")
        query = (f"query($path:ID!,$iid:String!{variable}){{project(fullPath:$path){{id mergeRequest(iid:$iid)"
                 f"{{title description diffHeadSha state mergedAt {commits} {discussions}}}}}}}")
        values = {"path": self.api.project, "iid": str(number)}
        if cursor is not None:
            values["after"] = cursor
        project = self._query(query, **values).get("project") or {}
        mr = project.get("mergeRequest")
        if mr is None:
            raise RemoteError("GitLab merge request is unavailable")
        mr["_project_id"] = project.get("id")
        return mr

    def _all(self, number: int, field: str, first: dict) -> list[dict]:
        nodes = list(first.get("nodes") or [])
        page = first.get("pageInfo") or {}
        while page.get("hasNextPage"):
            cursor = page.get("endCursor")
            if not cursor:
                raise IncompleteResponseError(f"GitLab MR {field} cursor is missing")
            next_page = self._mr(number, field, cursor)[field]
            nodes.extend(next_page.get("nodes") or [])
            page = next_page.get("pageInfo") or {}
        return nodes

    def get_commit(self, sha: str) -> Commit:
        require_sha(sha)
        cached = self.cache.get("commit", sha)
        if cached is not None:
            return cached
        item = self.api.get(f"/repository/commits/{sha}")
        if item.get("id") != sha:
            raise RemoteError("Requested GitLab commit SHA is unavailable")
        commit = Commit(sha, item["message"], item.get("committed_date"))
        self.cache.put("commit", sha, commit, immutable=True)
        return commit

    def _snapshot(self, number: int) -> tuple[Change, tuple[Commit, ...], tuple[TimelineEvent, ...], str | None]:
        cached = self.cache.get("mr_snapshot", str(number))
        if cached is not None:
            return cached
        mr = self._mr(number)
        if not mr.get("diffHeadSha"):
            raise RemoteError("GitLab MR head SHA is unavailable")
        commits = self._all(number, "commits", mr["commits"])
        discussions = self._all(number, "discussions", mr["discussions"])
        links = list(self.api.pages(f"/merge_requests/{number}/closes_issues"))
        snapshot = self._build_snapshot(number, mr, commits, discussions, links)
        self.cache.put("mr_snapshot", str(number), snapshot)
        return snapshot

    def _build_snapshot(self, number: int, mr: dict, commits: list[dict],
                        discussions: list[dict], links: list[dict]) -> tuple:
        self._validate_links(links, mr.get("_project_id"))
        change = Change(number, mr["title"], mr.get("description") or "", mr["diffHeadSha"],
                        mr["state"], tuple(item["iid"] for item in links))
        history = tuple(Commit(item["sha"], "\n\n".join(filter(None, [item.get("fullTitle"),
                               item.get("description")])), item.get("committedDate")) for item in commits)
        for item in history:
            self.cache.put("commit", item.sha, item, immutable=True)
        for item in links:
            if item.get("title") and item.get("description") is not None:
                self.cache.put("issue_summary", str(item["iid"]),
                               Issue(item["iid"], item["title"], item["description"]))
        comments = self._comments(discussions)
        return change, history, tuple(comments), mr.get("mergedAt")

    @staticmethod
    def _validate_links(links: list[dict], project_id: str | None) -> None:
        if not project_id:
            raise IncompleteResponseError("GitLab project ID is unavailable")
        local_id = project_id.rsplit("/", 1)[-1]
        if any(str(item.get("project_id")) != local_id or item.get("iid") is None for item in links):
            raise IncompleteResponseError("GitLab closing link is outside the configured project")

    def _comments(self, discussions: list[dict]) -> list[TimelineEvent]:
        notes = []
        for discussion in discussions:
            connection = discussion.get("notes") or {}
            if (connection.get("pageInfo") or {}).get("hasNextPage"):
                raise IncompleteResponseError("GitLab discussion exceeds one page")
            notes.extend(connection.get("nodes") or [])
        return [TimelineEvent("comment", item["createdAt"], message=item.get("body") or "",
                              author=(item.get("author") or {}).get("username"))
                for item in notes if not item.get("system") and item.get("createdAt")]

    def get_pr(self, number: int) -> Change:
        return self._snapshot(number)[0]

    def get_pr_timeline(self, number: int) -> PullRequestTimeline:
        change, commits, comments, merged_at = self._snapshot(number)
        return make_timeline(change, list(commits), list(comments), merged_at)

    def get_issue(self, number: int) -> Issue:
        cached = self.cache.get("issue", str(number))
        if cached is not None:
            return cached
        query = ("query($path:ID!,$iid:String!){project(fullPath:$path){id "
                 "issue(iid:$iid){title description}}}")
        project = self._query(query, path=self.api.project, iid=str(number)).get("project") or {}
        item = project.get("issue")
        if item is None:
            raise RemoteError("GitLab issue is unavailable")
        links = list(self.api.pages(f"/issues/{number}/closed_by"))
        self._validate_links(links, project.get("id"))
        issue = Issue(number, item["title"], item.get("description") or "",
                      tuple(link["iid"] for link in links))
        self.cache.put("issue", str(number), issue)
        return issue
