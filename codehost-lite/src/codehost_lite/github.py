"""Direct GitHub REST resource reads."""

from typing import Any

import httpx

from codehost_lite.http import CodeHostError, JsonApi, commit_ref, repository_path, resource_number
from codehost_lite.models import Commit, Issue, PullRequest


class GitHub:
    """Read one repository using a caller-owned client; create another instance for another repo."""

    def __init__(
        self,
        repository: str,
        client: httpx.Client,
        *,
        token: str = "",
        api_url: str = "https://api.github.com",
    ) -> None:
        """Use an optional token and an API root, including Enterprise /api/v3 roots."""
        headers = {"Accept": "application/vnd.github+json", "X-GitHub-Api-Version": "2022-11-28"}
        if token:
            headers["Authorization"] = f"Bearer {token}"
        root = f"{api_url.rstrip('/')}/repos/{repository_path(repository, github=True)}"
        self._api = JsonApi(client, root, headers)

    def get_pr(self, number: int) -> PullRequest:
        """Read PR metadata only, without downloading commits, comments or files."""
        return self._api.read(f"/pulls/{resource_number(number)}", _pull_request)

    def get_issue(self, number: int) -> Issue:
        """Read a true issue; GitHub PR numbers are rejected even by the issues endpoint."""
        return self._api.read(f"/issues/{resource_number(number)}", _issue)

    def get_commit(self, ref: str) -> Commit:
        """Read commit metadata by SHA, branch or tag; return the resolved SHA."""
        return self._api.read(f"/commits/{commit_ref(ref)}", _commit)


def _pull_request(data: dict[str, Any]) -> PullRequest:
    return PullRequest(
        number=data["number"],
        title=data["title"],
        description=data.get("body") or "",
        state="merged" if data["merged"] else data["state"],
        url=data["html_url"],
        head_sha=data["head"]["sha"],
        source_branch=data["head"]["ref"],
        target_branch=data["base"]["ref"],
    )


def _issue(data: dict[str, Any]) -> Issue:
    if "pull_request" in data:
        raise CodeHostError("This GitHub number is a pull request; use get_pr()")
    return Issue(
        number=data["number"],
        title=data["title"],
        description=data.get("body") or "",
        state=data["state"],
        url=data["html_url"],
    )


def _commit(data: dict[str, Any]) -> Commit:
    metadata = data["commit"]
    return Commit(
        sha=data["sha"],
        message=metadata["message"],
        url=data["html_url"],
        committed_at=(metadata.get("committer") or {}).get("date"),
        author_name=(metadata.get("author") or {}).get("name"),
    )
