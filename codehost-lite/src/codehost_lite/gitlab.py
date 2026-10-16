"""GitLab REST resource reads for GitLab.com and self-managed API v4 hosts."""

from typing import Any

import httpx

from codehost_lite.http import JsonApi, commit_ref, repository_path, resource_number
from codehost_lite.models import Commit, Issue, PullRequest


class GitLab:
    """Read one namespace/project with a caller-owned HTTP client and optional access token."""

    def __init__(
        self,
        repository: str,
        client: httpx.Client,
        *,
        token: str = "",
        api_url: str = "https://gitlab.com/api/v4",
    ) -> None:
        """Set api_url to the complete self-managed API root, including any URL prefix."""
        headers = {"Accept": "application/json"}
        if token:
            headers["PRIVATE-TOKEN"] = token
        root = f"{api_url.rstrip('/')}/projects/{repository_path(repository)}"
        self._api = JsonApi(client, root, headers)

    def get_pr(self, number: int) -> PullRequest:
        """Read an MR using its project-local IID, not its global database ID."""
        return self._api.read(f"/merge_requests/{resource_number(number)}", _pull_request)

    def get_issue(self, number: int) -> Issue:
        """Read an issue using its project-local IID."""
        return self._api.read(f"/issues/{resource_number(number)}", _issue)

    def get_commit(self, ref: str) -> Commit:
        """Read commit metadata by SHA, branch or tag; return the resolved SHA."""
        return self._api.read(f"/repository/commits/{commit_ref(ref)}", _commit)


def _pull_request(data: dict[str, Any]) -> PullRequest:
    return PullRequest(
        number=data["iid"],
        title=data["title"],
        description=data.get("description") or "",
        state=_state(data["state"]),
        url=data["web_url"],
        head_sha=data["sha"],
        source_branch=data["source_branch"],
        target_branch=data["target_branch"],
    )


def _issue(data: dict[str, Any]) -> Issue:
    return Issue(
        number=data["iid"],
        title=data["title"],
        description=data.get("description") or "",
        state=_state(data["state"]),
        url=data["web_url"],
    )


def _commit(data: dict[str, Any]) -> Commit:
    return Commit(
        sha=data["id"],
        message=data["message"],
        url=data["web_url"],
        committed_at=data.get("committed_date"),
        author_name=data.get("author_name"),
    )


def _state(state: str) -> str:
    return "open" if state == "opened" else state
